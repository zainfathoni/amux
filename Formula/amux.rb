class Amux < Formula
  desc "Restore Amp tmux workspaces from a simple TSV config"
  homepage "https://github.com/zainfathoni/amux"
  version "0.1.30"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.30/amux-v0.1.30-darwin-arm64.tar.gz"
      sha256 "d9c698cb6b4938287da170f4a00981580a7623e7ed611350bb0e0af77ebd5b59"
    else
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.30/amux-v0.1.30-darwin-amd64.tar.gz"
      sha256 "e305cba283bb48c1e831a8bee03b5f681620c69d314d7f3a64685b2a5b6ba939"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.30/amux-v0.1.30-linux-arm64.tar.gz"
      sha256 "2c18ffc12d6e1dd288e7d5c8fd9dc445abde10d4cd88d8b67b1c045b313897e8"
    else
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.30/amux-v0.1.30-linux-amd64.tar.gz"
      sha256 "a0569500de52099765a61d6aed76838765c852a07f2a05533a5a71f4e6397795"
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
