class Amux < Formula
  desc "Restore Amp tmux workspaces from a simple TSV config"
  homepage "https://github.com/zainfathoni/amux"
  version "0.1.32"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.32/amux-v0.1.32-darwin-arm64.tar.gz"
      sha256 "cf36fb0083b3fcc4812e5fe6abfa65f9173558cbcd9b31f63db338d228556bea"
    else
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.32/amux-v0.1.32-darwin-amd64.tar.gz"
      sha256 "3f5fc18ed89acfd53aa32e5413108dfac23e3488955081921ea1d16b8efb0e7b"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.32/amux-v0.1.32-linux-arm64.tar.gz"
      sha256 "e992fb59fc14bd0652f0ca9e3fa73d1c80092c2c4674e22f528314786835a087"
    else
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.32/amux-v0.1.32-linux-amd64.tar.gz"
      sha256 "e0421013b8744f75bfb116a40ac3c2833529b92e574038b6c27647e18bc03bcd"
    end
  end

  depends_on "tmux"

  def install
    bin.install "amux"
  end

  test do
    assert_match "amux v#{version}", shell_output("#{bin}/amux version")
  end
end
