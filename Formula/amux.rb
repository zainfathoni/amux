class Amux < Formula
  desc "Restore Amp tmux workspaces from a simple TSV config"
  homepage "https://github.com/zainfathoni/amux"
  version "0.1.31"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.31/amux-v0.1.31-darwin-arm64.tar.gz"
      sha256 "08dc52d8dfa6af8282033c5b2e9bcbc3184073c8311f33fdc3e5e62ff1123560"
    else
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.31/amux-v0.1.31-darwin-amd64.tar.gz"
      sha256 "99a20ab9dbf6d81101dbfea94ac048e3b58e0c5a9a644ccd15782c76890aa4f3"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.31/amux-v0.1.31-linux-arm64.tar.gz"
      sha256 "1b2d02bd8def26b8638dfe93e4b132d90fe344870dc964ea55d7cf577fe11388"
    else
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.31/amux-v0.1.31-linux-amd64.tar.gz"
      sha256 "c99c1d72473f34668392f013cbe8306aa6c83e5b1b0f53c04b249636724a7199"
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
