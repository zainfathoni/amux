package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
)

func (a app) executeWorkerCutover(in invocation, dir config.Directory) (*result.Envelope, error) {
	env := result.NewEnvelope(strings.Join(in.Path, " "), in.Options.DryRun)
	switch in.Command.Name {
	case "publish":
		if in.Selectors.Generation == "" {
			return &env, result.Request(errors.New("worker cutover publish requires --generation <label>"))
		}
		plan, err := config.PlanWorkerCutover(dir, in.Selectors.Generation)
		if err != nil {
			return &env, result.Preflight(err)
		}
		if in.Options.DryRun {
			out := workerCutoverOutcome(plan.Export, string(plan.Action), "")
			switch plan.Action {
			case config.WorkerCutoverPublishDuplicate:
				out.Message = "exact worker cutover generation is already published; immutable state would remain unchanged"
				env.Skipped = append(env.Skipped, out)
			case config.WorkerCutoverRecoverFence:
				out.Message = "would recover the matching workers/v2 downgrade fence from the immutable manifest"
				env.Planned = append(env.Planned, out)
			default:
				out.Message = "would durably publish the immutable worker cutover manifest, then install its workers/v2 downgrade fence"
				env.Planned = append(env.Planned, out)
			}
			if !in.Options.JSON {
				fmt.Fprintf(a.stdout, "WORKER-CUTOVER\tstate=planned\taction=%s\tgeneration=%s\tmanifest=%s\n", plan.Action, in.Selectors.Generation, plan.Export.ManifestSHA256)
			}
			return &env, nil
		}
		published, err := config.PublishWorkerCutover(dir, in.Selectors.Generation)
		if err != nil {
			return &env, result.Runtime(err)
		}
		out := workerCutoverOutcome(published.Export, string(published.Action), "")
		switch published.Action {
		case config.WorkerCutoverPublishDuplicate:
			out.Message = "exact worker cutover replay; immutable manifest and downgrade fence are unchanged"
			env.Skipped = append(env.Skipped, out)
		case config.WorkerCutoverRecoverFence:
			out.Message = "recovered the matching workers/v2 downgrade fence without rewriting the immutable manifest"
			env.Successful = append(env.Successful, out)
		default:
			out.Message = "published immutable worker cutover manifest and matching workers/v2 downgrade fence"
			env.Successful = append(env.Successful, out)
		}
		if !in.Options.JSON {
			fmt.Fprintf(a.stdout, "WORKER-CUTOVER\tstate=%s\taction=%s\tgeneration=%s\tmanifest=%s\n", published.Export.State, published.Action, published.Export.Generation, published.Export.ManifestSHA256)
		}
		return &env, nil
	case "status", "export":
		inspected, err := config.InspectWorkerCutover(dir)
		if err != nil {
			return &env, result.Preflight(err)
		}
		out := workerCutoverOutcome(inspected, in.Command.Name, "read-only worker cutover classification")
		env.Successful = append(env.Successful, out)
		if in.Options.JSON {
			return &env, nil
		}
		if in.Command.Name == "export" {
			encoder := json.NewEncoder(a.stdout)
			encoder.SetEscapeHTML(false)
			if err := encoder.Encode(inspected); err != nil {
				return &env, result.Runtime(err)
			}
			return &env, nil
		}
		fmt.Fprintf(a.stdout, "WORKER-CUTOVER\tstate=%s\tgeneration=%s\tmanifest=%s\tschema=%s\tworkers=%d\tadoptions=%d\tblockers=%d\n", inspected.State, inspected.Generation, inspected.ManifestSHA256, inspected.Registry.Schema, len(inspected.Workers), len(inspected.AdoptionOperations), len(inspected.Blockers))
		return &env, nil
	default:
		return &env, result.Request(fmt.Errorf("unsupported worker cutover command %s", in.Command.Name))
	}
}

func workerCutoverOutcome(export config.WorkerCutoverExport, action, message string) result.Outcome {
	return result.Outcome{
		Resource: result.ConfigResource(export.ManifestPath),
		Action:   action,
		Message:  message,
		Cutover:  &export,
	}
}
