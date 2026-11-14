# Run the generated formula's build and tests in a temporary prefix.
# Invoked by verify_homebrew.py through the installed Homebrew Ruby runtime.
require "formulary"
require "extend/ENV"
require "build_options"
require "open3"

# Exercise this formula's test block without installing Homebrew's developer
# gem bundle. Build and install methods still use Homebrew's implementation.
module FormulaFixtureAssertions
  def assert_equal(expected, actual)
    raise "Expected #{expected.inspect}, received #{actual.inspect}" unless expected == actual
  end

  def assert_match(expected, actual)
    raise "Missing #{expected.inspect} in #{actual.inspect}" unless actual.include?(expected)
  end

  def shell_output(command, expected_status = 0)
    output, status = Open3.capture2e(command)
    assert_equal(expected_status, status.exitstatus)
    output
  end
end

ENV.activate_extensions!(env: "std")
ENV.prepend_path "PATH", ARGV.fetch(4)
Pathname.activate_extensions!

formula = Formulary.factory(Pathname(ARGV.fetch(0)).expand_path)
formula.build = BuildOptions.new(Options.new, formula.options)
formula.extend(FormulaFixtureAssertions)
source = Pathname(ARGV.fetch(1)).expand_path
temporary_prefix = Pathname(ARGV.fetch(2)).expand_path
temporary_testpath = Pathname(ARGV.fetch(3)).expand_path
formula.define_singleton_method(:prefix) { |*_args| temporary_prefix }
formula.define_singleton_method(:testpath) { temporary_testpath }
(temporary_prefix/"bin").mkpath
temporary_testpath.mkpath

Dir.chdir(source) do
  formula.fetch
  ENV["GOPROXY"] = "off"
  formula.install
end
formula.test
puts "Generated Homebrew formula build and functional tests passed in a temporary prefix."
