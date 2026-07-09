class Amux < Formula
  desc "Restore Amp tmux workspaces from a simple TSV config"
  homepage "https://github.com/zainfathoni/amux"
  version "0.1.27"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.27/amux-v0.1.27-darwin-arm64.tar.gz"
      sha256 "c29e987252fb8433d18078054db7928f2191f91828509464c15d0743b1f74274"
    else
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.27/amux-v0.1.27-darwin-amd64.tar.gz"
      sha256 "a343474bb174afc3bc8aeb623f84e8a64096a1218b3528cc61a2f195bbc28740"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.27/amux-v0.1.27-linux-arm64.tar.gz"
      sha256 "34175090a6c921eea17bbf044be3de44afd3646e8978d5e1dd14c5bb683ac4ee"
    else
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.27/amux-v0.1.27-linux-amd64.tar.gz"
      sha256 "96a17298e77f5dfb39f0547b741b4a5242bbf36925a9e0edca2bac50d63fea4d"
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
