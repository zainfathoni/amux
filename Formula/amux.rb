class Amux < Formula
  desc "Restore Amp tmux workspaces from a simple TSV config"
  homepage "https://github.com/zainfathoni/amux"
  version "0.1.23"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.23/amux-v0.1.23-darwin-arm64.tar.gz"
      sha256 "659d3dd5063d49a008323cae7ebcf00fb216eb2ce3019c562b21e44d635a1c9f"
    else
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.23/amux-v0.1.23-darwin-amd64.tar.gz"
      sha256 "25e3ac53df5723838abab89b51f55558d55629edf0242256c5b27696d1c28d7c"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.23/amux-v0.1.23-linux-arm64.tar.gz"
      sha256 "fa547cd082bd9587df5ca28206d92e1dd369b2a33f50ba1c9a59593f3507bc51"
    else
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.23/amux-v0.1.23-linux-amd64.tar.gz"
      sha256 "47febeab88979422c0b16d5dcdfc60179d135ae7457d3895ccaf6e82277f726a"
    end
  end

  depends_on "tmux"

  def install
    bin.install "amux"
  end

  test do
    assert_match "amux #{version}", shell_output("#{bin}/amux version")
  end
end
