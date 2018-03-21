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
					Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{"ssh", "my_app", "--command", JavaDetectionCommand + "; if [ -f /tmp/heapdump-abcd-123456.hprof ]; then echo >&2 'Heap dump /tmp/heapdump-abcd-123456.hprof already exists'; exit 1; fi; JMAP_COMMAND=`find -executable -name jmap | head -1 | tr -d [:space:]`; if [ -n \"${JMAP_COMMAND}\" ]; then true; OUTPUT=$( ${JMAP_COMMAND} -dump:format=b,file=/tmp/heapdump-abcd-123456.hprof $(pidof java) ) || STATUS_CODE=$?; if [ ! -s /tmp/heapdump-abcd-123456.hprof ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; cat /tmp/heapdump-abcd-123456.hprof; exit 0; fi; JVMMON_COMMAND=`find -executable -name jvmmon | head -1 | tr -d [:space:]`; if [ -n \"${JVMMON_COMMAND}\" ]; then true; OUTPUT=$( ${JVMMON_COMMAND} -pid $(pidof java) -c \"dump heap\" ) || STATUS_CODE=$?; sleep 5; HEAP_DUMP_NAME=`find -name 'java_pid*.hprof' -printf '%T@ %p\\0' | sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\\0' '\\n' | head -n 1`; SIZE=-1; OLD_SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); while [ \"${SIZE}\" != \"${OLD_SIZE}\" ]; do sleep 3; SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); done; if [ ! -s \"${HEAP_DUMP_NAME}\" ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; cat ${HEAP_DUMP_NAME}; fi; rm -f /tmp/heapdump-abcd-123456.hprof; if [ -n \"${HEAP_DUMP_NAME}\" ]; then rm -f ${HEAP_DUMP_NAME} ${HEAP_DUMP_NAME%.*}.addons; fi"}))
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
					Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{"ssh", "my_app", "--app-instance-index", "4", "--command", JavaDetectionCommand + "; if [ -f /tmp/heapdump-abcd-123456.hprof ]; then echo >&2 'Heap dump /tmp/heapdump-abcd-123456.hprof already exists'; exit 1; fi; JMAP_COMMAND=`find -executable -name jmap | head -1 | tr -d [:space:]`; if [ -n \"${JMAP_COMMAND}\" ]; then true; OUTPUT=$( ${JMAP_COMMAND} -dump:format=b,file=/tmp/heapdump-abcd-123456.hprof $(pidof java) ) || STATUS_CODE=$?; if [ ! -s /tmp/heapdump-abcd-123456.hprof ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; cat /tmp/heapdump-abcd-123456.hprof; exit 0; fi; JVMMON_COMMAND=`find -executable -name jvmmon | head -1 | tr -d [:space:]`; if [ -n \"${JVMMON_COMMAND}\" ]; then true; OUTPUT=$( ${JVMMON_COMMAND} -pid $(pidof java) -c \"dump heap\" ) || STATUS_CODE=$?; sleep 5; HEAP_DUMP_NAME=`find -name 'java_pid*.hprof' -printf '%T@ %p\\0' | sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\\0' '\\n' | head -n 1`; SIZE=-1; OLD_SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); while [ \"${SIZE}\" != \"${OLD_SIZE}\" ]; do sleep 3; SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); done; if [ ! -s \"${HEAP_DUMP_NAME}\" ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; cat ${HEAP_DUMP_NAME}; fi; rm -f /tmp/heapdump-abcd-123456.hprof; if [ -n \"${HEAP_DUMP_NAME}\" ]; then rm -f ${HEAP_DUMP_NAME} ${HEAP_DUMP_NAME%.*}.addons; fi"}))
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
					Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{"ssh", "my_app", "--app-instance-index", "4", "--command", JavaDetectionCommand + "; " +
						"if [ -f /tmp/heapdump-abcd-123456.hprof ]; then echo >&2 'Heap dump /tmp/heapdump-abcd-123456.hprof already exists'; exit 1; fi; " +
						"JMAP_COMMAND=`find -executable -name jmap | head -1 | tr -d [:space:]`; if [ -n \"${JMAP_COMMAND}\" ]; then true; OUTPUT=$( ${JMAP_COMMAND} -dump:format=b,file=/tmp/heapdump-abcd-123456.hprof $(pidof java) ) || STATUS_CODE=$?; if [ ! -s /tmp/heapdump-abcd-123456.hprof ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; cat /tmp/heapdump-abcd-123456.hprof; exit 0; fi; " +
						"JVMMON_COMMAND=`find -executable -name jvmmon | head -1 | tr -d [:space:]`; if [ -n \"${JVMMON_COMMAND}\" ]; then true; OUTPUT=$( ${JVMMON_COMMAND} -pid $(pidof java) -c \"dump heap\" ) || STATUS_CODE=$?; sleep 5; HEAP_DUMP_NAME=`find -name 'java_pid*.hprof' -printf '%T@ %p\\0' | sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\\0' '\\n' | head -n 1`; SIZE=-1; OLD_SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); while [ \"${SIZE}\" != \"${OLD_SIZE}\" ]; do sleep 3; SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); done; if [ ! -s \"${HEAP_DUMP_NAME}\" ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; cat ${HEAP_DUMP_NAME}; fi"}))
				})

			})

			Context("with the --dry-run flag", func() {

				It("prints out the command line without executing the command", func(done Done) {
					defer close(done)

					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, []string{"java", "heap-dump", "my_app", "-i", "4", "-k", "-n"})
						return output, err
					})

					expectedOutput := "cf ssh my_app --app-instance-index 4 --command '" + JavaDetectionCommand + "; " +
						"if [ -f /tmp/heapdump-abcd-123456.hprof ]; then echo >&2 'Heap dump /tmp/heapdump-abcd-123456.hprof already exists'; exit 1; fi; " +
						"JMAP_COMMAND=`find -executable -name jmap | head -1 | tr -d [:space:]`; if [ -n \"${JMAP_COMMAND}\" ]; then true; OUTPUT=$( ${JMAP_COMMAND} -dump:format=b,file=/tmp/heapdump-abcd-123456.hprof $(pidof java) ) || STATUS_CODE=$?; if [ ! -s /tmp/heapdump-abcd-123456.hprof ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; cat /tmp/heapdump-abcd-123456.hprof; exit 0; fi; " +
						"JVMMON_COMMAND=`find -executable -name jvmmon | head -1 | tr -d [:space:]`; if [ -n \"${JVMMON_COMMAND}\" ]; then true; OUTPUT=$( ${JVMMON_COMMAND} -pid $(pidof java) -c \"dump heap\" ) || STATUS_CODE=$?; sleep 5; HEAP_DUMP_NAME=`find -name 'java_pid*.hprof' -printf '%T@ %p\\0' | sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\\0' '\\n' | head -n 1`; SIZE=-1; OLD_SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); while [ \"${SIZE}\" != \"${OLD_SIZE}\" ]; do sleep 3; SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); done; if [ ! -s \"${HEAP_DUMP_NAME}\" ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; cat ${HEAP_DUMP_NAME}; fi'"

					Expect(output).To(Equal(expectedOutput))
					Expect(err).To(BeNil())
					Expect(cliOutput).To(ContainSubstring(expectedOutput))

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
					Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{"ssh", "my_app", "--command", JavaDetectionCommand + "; " +
						"JSTACK_COMMAND=`find -executable -name jstack | head -1`; if [ -n \"${JSTACK_COMMAND}\" ]; then ${JSTACK_COMMAND} $(pidof java); exit 0; fi; " +
						"JVMMON_COMMAND=`find -executable -name jvmmon | head -1`; if [ -n \"${JVMMON_COMMAND}\" ]; then ${JVMMON_COMMAND} -pid $(pidof java) -c \"print stacktrace\"; fi"}))
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
					Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{"ssh", "my_app", "--app-instance-index", "4", "--command", JavaDetectionCommand + "; " +
						"JSTACK_COMMAND=`find -executable -name jstack | head -1`; if [ -n \"${JSTACK_COMMAND}\" ]; then ${JSTACK_COMMAND} $(pidof java); exit 0; fi; " +
						"JVMMON_COMMAND=`find -executable -name jvmmon | head -1`; if [ -n \"${JVMMON_COMMAND}\" ]; then ${JVMMON_COMMAND} -pid $(pidof java) -c \"print stacktrace\"; fi"}))
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

					expectedOutput := "cf ssh my_app --app-instance-index 4 --command '" + JavaDetectionCommand + "; " +
						"JSTACK_COMMAND=`find -executable -name jstack | head -1`; if [ -n \"${JSTACK_COMMAND}\" ]; then ${JSTACK_COMMAND} $(pidof java); exit 0; fi; " +
						"JVMMON_COMMAND=`find -executable -name jvmmon | head -1`; if [ -n \"${JVMMON_COMMAND}\" ]; then ${JVMMON_COMMAND} -pid $(pidof java) -c \"print stacktrace\"; fi'"

					Expect(output).To(Equal(expectedOutput))
					Expect(err).To(BeNil())
					Expect(cliOutput).To(ContainSubstring(expectedOutput))

					Expect(commandExecutor.ExecuteCallCount()).To(Equal(0))
				})

			})

		})

	})

})
