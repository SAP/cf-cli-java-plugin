package main_test

import (
	"strings"

	. "github.com/SAP/cf-cli-java-plugin"
	. "github.com/SAP/cf-cli-java-plugin/cmd/fakes"
	. "github.com/SAP/cf-cli-java-plugin/uuid/fakes"

	io_helpers "code.cloudfoundry.org/cli/util/testhelpers/io"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

type commandOutput struct {
	out string
	err error
}

func captureOutput(closure func() (string, error)) (string, error, string) {
	cliOutputChan := make(chan []string)
	defer close(cliOutputChan)

	cmdOutputChan := make(chan *commandOutput)
	defer close(cmdOutputChan)

	go func() {
		cliOutput := io_helpers.CaptureOutput(func() {
			output, err := closure()
			cmdOutputChan <- &commandOutput{out: output, err: err}
		})
		cliOutputChan <- cliOutput
	}()

	var cliOutput []string
	var cmdOutput *commandOutput

	Eventually(cmdOutputChan, 5).Should(Receive(&cmdOutput))

	Eventually(cliOutputChan).Should(Receive(&cliOutput))

	cliOutputString := strings.Join(cliOutput, "|")

	return cmdOutput.out, cmdOutput.err, cliOutputString
}

var _ = Describe("CfJavaPlugin", func() {

	Describe("Run", func() {

		var (
			subject         *JavaPlugin
			commandExecutor *FakeCommandExecutor
			uuidGenerator   *FakeUUIDGenerator
		)

		BeforeEach(func() {
			subject = &JavaPlugin{}
			commandExecutor = new(FakeCommandExecutor)
			uuidGenerator = new(FakeUUIDGenerator)

			uuidGenerator.GenerateReturns("abcd-123456")
		})

		Context("when invoked without arguments", func() {

			It("outputs an error and does not invoke cf ssh", func(done Done) {
				defer close(done)

				output, err, cliOutput := captureOutput(func() (string, error) {
					output, err := subject.DoRun(commandExecutor, uuidGenerator, []string{"java"})
					return output, err
				})

				Expect(output).To(BeEmpty())
				Expect(err.Error()).To(ContainSubstring("No command provided"))
				Expect(cliOutput).To(ContainSubstring("No command provided"))

				Expect(commandExecutor.ExecuteCallCount()).To(Equal(1))
				Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{"help", "java"}))
			})

		})

		Context("when invoked with too many arguments", func() {

			It("outputs an error and does not invoke cf ssh", func(done Done) {
				defer close(done)

				output, err, cliOutput := captureOutput(func() (string, error) {
					output, err := subject.DoRun(commandExecutor, uuidGenerator, []string{"java", "heap-dump", "my_app", "ciao"})
					return output, err
				})

				Expect(output).To(BeEmpty())
				Expect(err.Error()).To(ContainSubstring("Too many arguments provided: ciao"))
				Expect(cliOutput).To(ContainSubstring("Too many arguments provided: ciao"))

				Expect(commandExecutor.ExecuteCallCount()).To(Equal(1))
				Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{"help", "java"}))
			})

		})

		Context("when invoked with an unknown command", func() {

			It("outputs an error and does not invoke cf ssh", func(done Done) {
				defer close(done)

				output, err, cliOutput := captureOutput(func() (string, error) {
					output, err := subject.DoRun(commandExecutor, uuidGenerator, []string{"java", "UNKNOWN_COMMAND"})
					return output, err
				})

				Expect(output).To(BeEmpty())
				Expect(err.Error()).To(ContainSubstring("Unrecognized command \"UNKNOWN_COMMAND\": supported commands are 'heap-dump' and 'thread-dump'"))
				Expect(cliOutput).To(ContainSubstring("Unrecognized command \"UNKNOWN_COMMAND\": supported commands are 'heap-dump' and 'thread-dump'"))

				Expect(commandExecutor.ExecuteCallCount()).To(Equal(1))
				Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{"help", "java"}))
			})

		})

		Context("when invoked to generate a heap-dump", func() {

			Context("without application name", func() {

				It("outputs an error and does not invoke cf ssh", func(done Done) {
					defer close(done)

					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, []string{"java", "heap-dump"})
						return output, err
					})

					Expect(output).To(BeEmpty())
					Expect(err.Error()).To(ContainSubstring("No application name provided"))
					Expect(cliOutput).To(ContainSubstring("No application name provided"))

					Expect(commandExecutor.ExecuteCallCount()).To(Equal(1))
					Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{"help", "java"}))
				})

			})

			Context("with too many arguments", func() {

				It("outputs an error and does not invoke cf ssh", func(done Done) {
					defer close(done)

					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, []string{"java", "heap-dump", "my_app", "my_file", "ciao"})
						return output, err
					})

					Expect(output).To(BeEmpty())
					Expect(err.Error()).To(ContainSubstring("Too many arguments provided: my_file, ciao"))
					Expect(cliOutput).To(ContainSubstring("Too many arguments provided: my_file, ciao"))

					Expect(commandExecutor.ExecuteCallCount()).To(Equal(1))
					Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{"help", "java"}))
				})

			})

			Context("with just the app name", func() {

				It("invokes cf ssh with the basic commands", func(done Done) {
					defer close(done)

					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, []string{"java", "heap-dump", "my_app"})
						return output, err
					})

					Expect(output).To(BeEmpty())
					Expect(err).To(BeNil())
					Expect(cliOutput).To(Equal(""))

					Expect(commandExecutor.ExecuteCallCount()).To(Equal(1))
					Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{"ssh", "my_app", "--command", JavaDetectionCommand + "$(find -executable -name jmap | head -1) -dump:format=b,file=/tmp/heapdump-abcd-123456.hprof $(pidof java) > /dev/null; cat /tmp/heapdump-abcd-123456.hprof; rm -f /tmp/heapdump-abcd-123456.hprof"}))
				})

			})

			Context("for a container with index > 0", func() {

				It("invokes cf ssh with the basic commands", func(done Done) {
					defer close(done)

					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, []string{"java", "heap-dump", "my_app", "-i", "4"})
						return output, err
					})

					Expect(output).To(BeEmpty())
					Expect(err).To(BeNil())
					Expect(cliOutput).To(Equal(""))

					Expect(commandExecutor.ExecuteCallCount()).To(Equal(1))
					Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{"ssh", "my_app", "--app-instance-index", "4", "--command", JavaDetectionCommand + "$(find -executable -name jmap | head -1) -dump:format=b,file=/tmp/heapdump-abcd-123456.hprof $(pidof java) > /dev/null; cat /tmp/heapdump-abcd-123456.hprof; rm -f /tmp/heapdump-abcd-123456.hprof"}))
				})

			})

			Context("with the --keep flag", func() {

				It("keeps the heap-dump on the container", func(done Done) {
					defer close(done)

					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, []string{"java", "heap-dump", "my_app", "-i", "4", "-k"})
						return output, err
					})

					Expect(output).To(BeEmpty())
					Expect(err).To(BeNil())
					Expect(cliOutput).To(Equal(""))

					Expect(commandExecutor.ExecuteCallCount()).To(Equal(1))
					Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{"ssh", "my_app", "--app-instance-index", "4", "--command", JavaDetectionCommand + "$(find -executable -name jmap | head -1) -dump:format=b,file=/tmp/heapdump-abcd-123456.hprof $(pidof java) > /dev/null; cat /tmp/heapdump-abcd-123456.hprof"}))
				})

			})

			Context("with the --dry-run flag", func() {

				It("prints out the command line without executing the command", func(done Done) {
					defer close(done)

					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, []string{"java", "heap-dump", "my_app", "-i", "4", "-k", "-n"})
						return output, err
					})

					Expect(output).To(Equal("cf ssh my_app --app-instance-index 4 --command '" + JavaDetectionCommand + "$(find -executable -name jmap | head -1) -dump:format=b,file=/tmp/heapdump-abcd-123456.hprof $(pidof java) > /dev/null; cat /tmp/heapdump-abcd-123456.hprof'"))
					Expect(err).To(BeNil())
					Expect(cliOutput).To(ContainSubstring("cf ssh my_app --app-instance-index 4 --command '" + JavaDetectionCommand + "$(find -executable -name jmap | head -1) -dump:format=b,file=/tmp/heapdump-abcd-123456.hprof $(pidof java) > /dev/null; cat /tmp/heapdump-abcd-123456.hprof'"))

					Expect(commandExecutor.ExecuteCallCount()).To(Equal(0))
				})

			})

		})

		Context("when invoked to generate a thread-dump", func() {

			Context("without application name", func() {

				It("outputs an error and does not invoke cf ssh", func(done Done) {
					defer close(done)

					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, []string{"java", "thread-dump"})
						return output, err
					})

					Expect(output).To(BeEmpty())
					Expect(err.Error()).To(ContainSubstring("No application name provided"))
					Expect(cliOutput).To(ContainSubstring("No application name provided"))

					Expect(commandExecutor.ExecuteCallCount()).To(Equal(1))
					Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{"help", "java"}))
				})

			})

			Context("with too many arguments", func() {

				It("outputs an error and does not invoke cf ssh", func(done Done) {
					defer close(done)

					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, []string{"java", "thread-dump", "my_app", "my_file", "ciao"})
						return output, err
					})

					Expect(output).To(BeEmpty())
					Expect(err.Error()).To(ContainSubstring("Too many arguments provided: my_file, ciao"))
					Expect(cliOutput).To(ContainSubstring("Too many arguments provided: my_file, ciao"))

					Expect(commandExecutor.ExecuteCallCount()).To(Equal(1))
					Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{"help", "java"}))
				})

			})

			Context("with just the app name", func() {

				It("invokes cf ssh with the basic commands", func(done Done) {
					defer close(done)

					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, []string{"java", "thread-dump", "my_app"})
						return output, err
					})

					Expect(output).To(BeEmpty())
					Expect(err).To(BeNil())
					Expect(cliOutput).To(Equal(""))

					Expect(commandExecutor.ExecuteCallCount()).To(Equal(1))
					Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{"ssh", "my_app", "--command", JavaDetectionCommand + "$(find -executable -name jstack | head -1) $(pidof java)"}))
				})

			})

			Context("for a container with index > 0", func() {

				It("invokes cf ssh with the basic commands", func(done Done) {
					defer close(done)

					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, []string{"java", "thread-dump", "my_app", "-i", "4"})
						return output, err
					})

					Expect(output).To(BeEmpty())
					Expect(err).To(BeNil())
					Expect(cliOutput).To(Equal(""))

					Expect(commandExecutor.ExecuteCallCount()).To(Equal(1))
					Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{"ssh", "my_app", "--app-instance-index", "4", "--command", JavaDetectionCommand + "$(find -executable -name jstack | head -1) $(pidof java)"}))
				})

			})

			Context("with the --keep flag", func() {

				It("fails", func(done Done) {
					defer close(done)

					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, []string{"java", "thread-dump", "my_app", "-i", "4", "-k"})
						return output, err
					})

					Expect(output).To(BeEmpty())
					Expect(err.Error()).To(ContainSubstring("The flag \"keep\" is not supported for thread-dumps"))
					Expect(cliOutput).To(ContainSubstring("The flag \"keep\" is not supported for thread-dumps"))

					Expect(commandExecutor.ExecuteCallCount()).To(Equal(1))
					Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{"help", "java"}))
				})

			})

			Context("with the --dry-run flag", func() {

				It("prints out the command line without executing the command", func(done Done) {
					defer close(done)

					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, []string{"java", "thread-dump", "my_app", "-i", "4", "-n"})
						return output, err
					})

					Expect(output).To(Equal("cf ssh my_app --app-instance-index 4 --command '" + JavaDetectionCommand + "$(find -executable -name jstack | head -1) $(pidof java)'"))
					Expect(err).To(BeNil())
					Expect(cliOutput).To(ContainSubstring("cf ssh my_app --app-instance-index 4 --command '" + JavaDetectionCommand + "$(find -executable -name jstack | head -1) $(pidof java)'"))

					Expect(commandExecutor.ExecuteCallCount()).To(Equal(0))
				})

			})

		})

	})

})
