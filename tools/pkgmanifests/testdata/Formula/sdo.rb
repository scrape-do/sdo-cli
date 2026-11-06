class Sdo < Formula
  desc "Scrape.do command-line client, with a bundled MCP server"
  homepage "https://scrape.do"
  license "Apache-2.0"

  livecheck do
    url :stable
    strategy :github_latest
  end

  on_macos do
    on_arm do
      url "https://github.com/scrape-do/sdo-cli/releases/download/v0.2.0/sdo_0.2.0_darwin_arm64.tar.gz"
      sha256 "264f42b6fbeaa140c79cdaed62b644ced737bb994eb51250ae7bea8bcd0d954d"
    end
    on_intel do
      url "https://github.com/scrape-do/sdo-cli/releases/download/v0.2.0/sdo_0.2.0_darwin_amd64.tar.gz"
      sha256 "904d2734b70f4f195fc87a98d9b58e306d5ca45f62971b1073d9017a878ed478"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/scrape-do/sdo-cli/releases/download/v0.2.0/sdo_0.2.0_linux_arm64.tar.gz"
      sha256 "f15faaeea486e9e1eb20e2d1ab3375c84158b59283b61c5567c2f1befd1742d0"
    end
    on_intel do
      url "https://github.com/scrape-do/sdo-cli/releases/download/v0.2.0/sdo_0.2.0_linux_amd64.tar.gz"
      sha256 "6c3f0fcb296e9d7a10ae24a71bf1edeaf9fb4e66a1bc0b5ad84ec360cd54e154"
    end
  end

  def install
    bin.install "sdo"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/sdo --version")
  end
end
