class LegalscoutDev < Formula
  desc "Local development build of the LegalScout CLI"
  homepage "https://github.com/Duan-JM/LegalScout"
  license "Apache-2.0"
  head "file://#{File.expand_path("..", __dir__)}", branch: "main", using: :git

  depends_on "go" => :build

  def install
    system "go", "build", "-trimpath", "-o", bin/"legalscout", "./cmd/legalscout"
  end

  test do
    assert_match "律师批量核查执行器", shell_output("#{bin}/legalscout --help")
  end
end
