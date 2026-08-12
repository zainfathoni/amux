package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	retirementRecordIDDomain  = "amux.retirement.record-id.v1\x00"
	retirementEventDomain     = "amux.retirement.event.v1\x00"
	retirementOperationDomain = "amux.retirement.operation.v1\x00"
	retirementManifestDomain  = "amux.retirement.manifest.v1\x00"
	retirementIdentityDomain  = "amux.retirement.identity.v1\x00"
)

var canonicalInteger = regexp.MustCompile(`^(0|-[1-9][0-9]*|[1-9][0-9]*)$`)

type canonicalKind uint8

const (
	canonicalString canonicalKind = iota
	canonicalIntegerKind
	canonicalBool
	canonicalArray
	canonicalObject
)

type canonicalValue struct {
	kind canonicalKind
	str  string
	bool bool
	arr  []canonicalValue
	obj  map[string]canonicalValue
}

func cString(value string) canonicalValue { return canonicalValue{kind: canonicalString, str: value} }
func cInt(value int64) canonicalValue {
	return canonicalValue{kind: canonicalIntegerKind, str: strconv.FormatInt(value, 10)}
}
func cBool(value bool) canonicalValue { return canonicalValue{kind: canonicalBool, bool: value} }
func cArray(values ...canonicalValue) canonicalValue {
	return canonicalValue{kind: canonicalArray, arr: values}
}
func cObject(values map[string]canonicalValue) canonicalValue {
	return canonicalValue{kind: canonicalObject, obj: values}
}

func parseCanonicalJSON(data []byte) (canonicalValue, error) {
	if !utf8.Valid(data) {
		return canonicalValue{}, errors.New("JSON is not valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := readCanonicalValue(decoder, 0)
	if err != nil {
		return canonicalValue{}, err
	}
	if token, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return canonicalValue{}, fmt.Errorf("unexpected trailing JSON token %v", token)
		}
		return canonicalValue{}, fmt.Errorf("trailing JSON data: %w", err)
	}
	return value, nil
}

func readCanonicalValue(decoder *json.Decoder, depth int) (canonicalValue, error) {
	if depth > 32 {
		return canonicalValue{}, errors.New("JSON nesting exceeds 32 levels")
	}
	token, err := decoder.Token()
	if err != nil {
		return canonicalValue{}, err
	}
	switch value := token.(type) {
	case string:
		return cString(value), nil
	case json.Number:
		text := string(value)
		if !canonicalInteger.MatchString(text) {
			return canonicalValue{}, fmt.Errorf("non-integer JSON number %q", text)
		}
		return canonicalValue{kind: canonicalIntegerKind, str: text}, nil
	case bool:
		return cBool(value), nil
	case nil:
		return canonicalValue{}, errors.New("JSON null is not permitted")
	case json.Delim:
		switch value {
		case '[':
			items := make([]canonicalValue, 0)
			for decoder.More() {
				item, err := readCanonicalValue(decoder, depth+1)
				if err != nil {
					return canonicalValue{}, err
				}
				items = append(items, item)
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return canonicalValue{}, errors.New("unterminated JSON array")
			}
			return cArray(items...), nil
		case '{':
			fields := make(map[string]canonicalValue)
			for decoder.More() {
				nameToken, err := decoder.Token()
				if err != nil {
					return canonicalValue{}, err
				}
				name, ok := nameToken.(string)
				if !ok {
					return canonicalValue{}, errors.New("JSON object key is not a string")
				}
				if _, duplicate := fields[name]; duplicate {
					return canonicalValue{}, fmt.Errorf("duplicate JSON field %q", name)
				}
				field, err := readCanonicalValue(decoder, depth+1)
				if err != nil {
					return canonicalValue{}, err
				}
				fields[name] = field
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return canonicalValue{}, errors.New("unterminated JSON object")
			}
			return cObject(fields), nil
		}
	}
	return canonicalValue{}, fmt.Errorf("unsupported JSON token %T", token)
}

func encodeCanonical(value canonicalValue) ([]byte, error) {
	var out []byte
	return appendCanonical(out, value)
}

func appendCanonical(out []byte, value canonicalValue) ([]byte, error) {
	switch value.kind {
	case canonicalString:
		if !utf8.ValidString(value.str) {
			return nil, errors.New("canonical string is not valid UTF-8")
		}
		encoded, err := json.Marshal(value.str)
		if err != nil {
			return nil, err
		}
		return append(out, encoded...), nil
	case canonicalIntegerKind:
		if !canonicalInteger.MatchString(value.str) {
			return nil, errors.New("invalid canonical integer")
		}
		return append(out, value.str...), nil
	case canonicalBool:
		return strconv.AppendBool(out, value.bool), nil
	case canonicalArray:
		out = append(out, '[')
		for index, item := range value.arr {
			if index != 0 {
				out = append(out, ',')
			}
			var err error
			out, err = appendCanonical(out, item)
			if err != nil {
				return nil, err
			}
		}
		return append(out, ']'), nil
	case canonicalObject:
		keys := make([]string, 0, len(value.obj))
		for key := range value.obj {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out = append(out, '{')
		for index, key := range keys {
			if !utf8.ValidString(key) {
				return nil, errors.New("canonical object key is not valid UTF-8")
			}
			if index != 0 {
				out = append(out, ',')
			}
			encodedKey, err := json.Marshal(key)
			if err != nil {
				return nil, err
			}
			out = append(out, encodedKey...)
			out = append(out, ':')
			out, err = appendCanonical(out, value.obj[key])
			if err != nil {
				return nil, err
			}
		}
		return append(out, '}'), nil
	default:
		return nil, errors.New("invalid canonical JSON value")
	}
}

func domainDigest(domain string, value canonicalValue) (string, error) {
	canonical, err := encodeCanonical(value)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(domain))
	_, _ = hash.Write(canonical)
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)) + ";domain=" + strings.TrimSuffix(domain, "\x00"), nil
}

func IdentityCommitment(kind, value string) (string, error) {
	if err := validateDiscriminator("identity kind", kind); err != nil {
		return "", err
	}
	if value == "" || len(value) > retirementMaxIdentityBytes {
		return "", errors.New("identity value must be non-empty and bounded")
	}
	value = norm.NFC.String(value)
	return domainDigest(retirementIdentityDomain, cObject(map[string]canonicalValue{
		"kind": cString(kind), "value": cString(value),
	}))
}

func RetirementRecordCommitment(recordID string) (string, error) {
	if err := ValidateRetirementRecordID(recordID); err != nil {
		return "", err
	}
	return domainDigest(retirementRecordIDDomain, cObject(map[string]canonicalValue{"record_id": cString(recordID)}))
}

func validateNFC(name, value string) error {
	if !utf8.ValidString(value) || !norm.NFC.IsNormalString(value) {
		return fmt.Errorf("%s must be valid NFC UTF-8", name)
	}
	return nil
}

func validateDigest(name, value, domain string) error {
	wantSuffix := ";domain=" + strings.TrimSuffix(domain, "\x00")
	if len(value) != len("sha256:")+sha256.Size*2+len(wantSuffix) || !strings.HasPrefix(value, "sha256:") || !strings.HasSuffix(value, wantSuffix) {
		return fmt.Errorf("%s has the wrong digest algorithm or domain", name)
	}
	hexPart := value[len("sha256:") : len("sha256:")+sha256.Size*2]
	_, err := hex.DecodeString(hexPart)
	if err != nil || hexPart != string(bytes.ToLower([]byte(hexPart))) {
		return fmt.Errorf("%s must be sha256 plus 64 lowercase hex characters", name)
	}
	return nil
}

func exactObject(value canonicalValue, names ...string) (map[string]canonicalValue, error) {
	if value.kind != canonicalObject {
		return nil, errors.New("expected JSON object")
	}
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		wanted[name] = true
	}
	for name := range value.obj {
		if !wanted[name] {
			return nil, fmt.Errorf("unknown JSON field %q", name)
		}
	}
	for _, name := range names {
		if _, ok := value.obj[name]; !ok {
			return nil, fmt.Errorf("missing JSON field %q", name)
		}
	}
	return value.obj, nil
}

func stringField(fields map[string]canonicalValue, name string) (string, error) {
	value, ok := fields[name]
	if !ok || value.kind != canonicalString {
		return "", fmt.Errorf("field %q must be a string", name)
	}
	return value.str, nil
}

func integerField(fields map[string]canonicalValue, name string) (int64, error) {
	value, ok := fields[name]
	if !ok || value.kind != canonicalIntegerKind {
		return 0, fmt.Errorf("field %q must be an integer", name)
	}
	parsed, err := strconv.ParseInt(value.str, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("field %q integer is out of range", name)
	}
	return parsed, nil
}
