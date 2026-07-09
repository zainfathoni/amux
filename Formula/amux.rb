class Amux < Formula
  desc "Restore Amp tmux workspaces from a simple TSV config"
  homepage "https://github.com/zainfathoni/amux"
  version "0.1.24"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.24/amux-v0.1.24-darwin-arm64.tar.gz"
      sha256 "6badbe429eb3e01d56ea54ab226ce5c676907143f09370b2b73ac1d5bf8faa0f"
    else
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.24/amux-v0.1.24-darwin-amd64.tar.gz"
      sha256 "8477f736bb37f712362a7e172a9553ba71e2a1d5c0388f1170eaf05c52622879"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.24/amux-v0.1.24-linux-arm64.tar.gz"
      sha256 "e6d1d44fc66a055667b8bb520c6c5ff78b4459cbd1c5fa226dddc08a63fdb283"
    else
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.24/amux-v0.1.24-linux-amd64.tar.gz"
      sha256 "57acefd346736476e5ff4e1a2e6407fa2b982dd85ba4305218c234719072a270"
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
