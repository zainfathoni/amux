class Amux < Formula
  desc "Restore Amp tmux workspaces from a simple TSV config"
  homepage "https://github.com/zainfathoni/amux"
  version "0.1.26"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.26/amux-v0.1.26-darwin-arm64.tar.gz"
      sha256 "84a20cb331c9a1c45de98e95cc894910e7bd85de973a32bf04f6a6c30b6c92a3"
    else
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.26/amux-v0.1.26-darwin-amd64.tar.gz"
      sha256 "9e4ae202edf3f75110301bf4f4962e1a3abfbff00017caaa41092ea26c1ccadc"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.26/amux-v0.1.26-linux-arm64.tar.gz"
      sha256 "30240d534d6e3ec95d3211634667cc1fd9859b055b8fc6a482d68896a460e682"
    else
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.26/amux-v0.1.26-linux-amd64.tar.gz"
      sha256 "0161d5b94706e10a13d5d9247028de5f15e592184df2a4ca3f16aac0a5a723d7"
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
