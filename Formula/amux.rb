class Amux < Formula
  desc "Restore Amp tmux workspaces from a simple TSV config"
  homepage "https://github.com/zainfathoni/amux"
  version "0.1.28"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.28/amux-v0.1.28-darwin-arm64.tar.gz"
      sha256 "88b23ed389594c38f84f2d09d461522cb3a58f499f02dbb147bc6a6256ab2b1a"
    else
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.28/amux-v0.1.28-darwin-amd64.tar.gz"
      sha256 "edefa14613846cd3f32c61e7a3851748df1db967be97e238c59f757956e8b8f9"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.28/amux-v0.1.28-linux-arm64.tar.gz"
      sha256 "2ec6b77c860e4be2d12457b85538736ac5831e1eccd72c242b01b941e0d91f67"
    else
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.28/amux-v0.1.28-linux-amd64.tar.gz"
      sha256 "586e347c820c36a5a40120ba016d7327d280f5c680644fe9099d6c22d053409c"
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
