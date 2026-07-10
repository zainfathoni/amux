class Amux < Formula
  desc "Restore Amp tmux workspaces from a simple TSV config"
  homepage "https://github.com/zainfathoni/amux"
  version "0.1.29"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.29/amux-v0.1.29-darwin-arm64.tar.gz"
      sha256 "ae4dbb16081901ec62f0ed723361d10f5c8c8e935cbc837d2b9278c65b2a887b"
    else
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.29/amux-v0.1.29-darwin-amd64.tar.gz"
      sha256 "35f4e6081bfd493ef2602350a0596db015fd37a331977b484c0b033bc50f7947"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.29/amux-v0.1.29-linux-arm64.tar.gz"
      sha256 "a92c44056b7f3e444a938c63122b23992a6e5e3ebf5b323f019f92ff2dc5b23e"
    else
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.29/amux-v0.1.29-linux-amd64.tar.gz"
      sha256 "ef77c579657f9b452b373978572f52cd00d15bfaf922644e7b00ddb13cea5c8a"
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
