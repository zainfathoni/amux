class Amux < Formula
  desc "Restore Amp tmux workspaces from a simple TSV config"
  homepage "https://github.com/zainfathoni/amux"
  version "0.1.25"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.25/amux-v0.1.25-darwin-arm64.tar.gz"
      sha256 "254f34ce27dbc5968d9c9feab39b43bb3bbf1e770beba07107bd6e995c737636"
    else
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.25/amux-v0.1.25-darwin-amd64.tar.gz"
      sha256 "b747978aa901dcf8801202631857487869d92ef0a26704374544c3883cc705cc"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.25/amux-v0.1.25-linux-arm64.tar.gz"
      sha256 "b82c253978ef22ea37f0a1be4744321b8ee728106c21d860cff52e7aa56178af"
    else
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.25/amux-v0.1.25-linux-amd64.tar.gz"
      sha256 "1b39a5dd0c54fdb3d33777945c266fdf3c7133ebd55cc6b6598e04c20af24f0d"
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
