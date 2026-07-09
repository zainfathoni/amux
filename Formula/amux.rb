class Amux < Formula
  desc "Restore Amp tmux workspaces from a simple TSV config"
  homepage "https://github.com/zainfathoni/amux"
  version "0.1.22"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.22/amux-v0.1.22-darwin-arm64.tar.gz"
      sha256 "fe036fbe38f6ed73e9402508bcbf8802e4acbcfdf4809f2282a8e793edf60918"
    else
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.22/amux-v0.1.22-darwin-amd64.tar.gz"
      sha256 "75590356890799aed41b01ca896347d2e3f400d2f44e6d9aabad7a1b5f102f9b"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.22/amux-v0.1.22-linux-arm64.tar.gz"
      sha256 "3222a9c58bf84d55ac2e79a666c4db2bbee45f040d1f31022e16a26296d75937"
    else
      url "https://github.com/zainfathoni/amux/releases/download/v0.1.22/amux-v0.1.22-linux-amd64.tar.gz"
      sha256 "12704fc20df55417f104833c48654218ded31f6ff8975b7baefdaf9ae31fdf43"
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
