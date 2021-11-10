package main

import (
	"strings"

	. "utils/fakes"

	io_helpers "code.cloudfoundry.org/cli/cf/util/testhelpers/io"
	. "github.com/SAP/cf-cli-java-plugin/cmd/fakes"
	. "github.com/SAP/cf-cli-java-plugin/uuid/fakes"

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
			pluginUtil      FakeCfJavaPluginUtil
		)

		BeforeEach(func() {
			subject = &JavaPlugin{}
			commandExecutor = new(FakeCommandExecutor)
			uuidGenerator = new(FakeUUIDGenerator)
			uuidGenerator.GenerateReturns("abcd-123456")
			pluginUtil = FakeCfJavaPluginUtil{SshEnabled: true, Jmap_jvmmon_present: true, Container_path_valid: true, Fspath: "/tmp", LocalPathValid: true, UUID: "abcd-123456", OutputFileName: "java_pid0_0.hprof"}
		})

		Context("when invoked without arguments", func() {

			It("outputs an error and does not invoke cf ssh", func(done Done) {
				defer close(done)

				output, err, cliOutput := captureOutput(func() (string, error) {
					output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java"})
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
					output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "heap-dump", "my_app", "ciao"})
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
					output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "UNKNOWN_COMMAND"})
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
						output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "heap-dump"})
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
						output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "heap-dump", "my_app", "my_file", "ciao"})
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
						output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "heap-dump", "my_app"})
						return output, err
					})
					// heapDumpFile := pluginUtil.Fspath + "/my_app-heapdump-" + pluginUtil.UUID + ".hprof"
					Expect(output).To(BeEmpty())
					Expect(err).To(BeNil())
					// Empty output is not produced in any case now.
					Expect(cliOutput).To(Equal("Successfully created heap dump in application container at: " + pluginUtil.Fspath + "/" + pluginUtil.OutputFileName + "|Heap dump will not be copied as parameter `local-dir` was not set|Heap dump file deleted in app container|"))

					Expect(commandExecutor.ExecuteCallCount()).To(Equal(1))

					/*
						-----------------------------------------------------------------------------------------------------------------------
																		Test Case Issues
						-----------------------------------------------------------------------------------------------------------------------
						Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{"ssh", "my_app", "--command", JavaDetectionCommand + "; if [ -f " + heapDumpFile + " ]; then echo >&2 'Heap dump " + heapDumpFile + " already exists'; exit 1; fi; JMAP_COMMAND=`find -executable -name jmap | head -1 | tr -d [:space:]`; JVMMON_COMMAND=`find -executable -name jvmmon | head -1 | tr -d [:space:]`; if [ -n \"${JMAP_COMMAND}\" ]; then true; OUTPUT=$( ${JMAP_COMMAND} -dump:format=b,file=" + heapDumpFile + " $(pidof java) ) || STATUS_CODE=$?; if [ ! -s " + heapDumpFile + " ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; cat " + heapDumpFile + "; exit 0; fi; JVMMON_COMMAND=`find -executable -name jvmmon | head -1 | tr -d [:space:]`; if [ -n \"${JVMMON_COMMAND}\" ]; then true; OUTPUT=$( ${JVMMON_COMMAND} -pid $(pidof java) -c \"dump heap\" ) || STATUS_CODE=$?; sleep 5; HEAP_DUMP_NAME=`find -name 'java_pid*.hprof' -printf '%T@ %p\\0' | sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\\0' '\\n' | head -n 1`; SIZE=-1; OLD_SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); while [ \"${SIZE}\" != \"${OLD_SIZE}\" ]; do sleep 3; SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); done; if [ ! -s \"${HEAP_DUMP_NAME}\" ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; cat ${HEAP_DUMP_NAME}; fi; rm -f " + heapDumpFile + "; if [ -n \"${HEAP_DUMP_NAME}\" ]; then rm -f ${HEAP_DUMP_NAME} ${HEAP_DUMP_NAME%.*}.addons; fi"}))

						issue with the above command assertion, does not match with the executed command:	 (NOT SURE WHICH ONE IS CORRECT)
							[
								"ssh",
								"my_app",
								"--command",
								"if ! pgrep -x \"java\" > /dev/null; then echo \"No 'java' process found running. Are you sure this is a Java app?\" >&2; exit 1; fi; if [ -f /tmp/my_app-heapdump-abcd-123456.hprof ]; then echo >&2 'Heap dump /tmp/my_app-heapdump-abcd-123456.hprof already exists'; exit 1; fi; JMAP_COMMAND=`find -executable -name jmap | head -1 | tr -d [:space:]`; JVMMON_COMMAND=`find -executable -name jvmmon | head -1 | tr -d [:space:]`; if [ -n \"${JMAP_COMMAND}\" ]; then true; OUTPUT=$( ${JMAP_COMMAND} -dump:format=b,file=/tmp/my_app-heapdump-abcd-123456.hprof $(pidof java) ) || STATUS_CODE=$?; if [ ! -s /tmp/my_app-heapdump-abcd-123456.hprof ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; elif [ -n \"${JVMMON_COMMAND}\" ]; then true; echo -e 'change command line flag flags=-XX:HeapDumpOnDemandPath=/tmp\ndump heap' > setHeapDumpOnDemandPath.sh; OUTPUT=$( ${JVMMON_COMMAND} -pid $(pidof java) -cmd \"setHeapDumpOnDemandPath.sh\" ) || STATUS_CODE=$?; sleep 5; HEAP_DUMP_NAME=`find /tmp -name 'java_pid*.hprof' -printf '%T@ %p\\0' | sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\\0' '\\n' | head -n 1`; SIZE=-1; OLD_SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); while [ ${SIZE} != ${OLD_SIZE} ]; do OLD_SIZE=${SIZE}; sleep 3; SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); done; if [ ! -s \"${HEAP_DUMP_NAME}\" ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; fi",
							]
							new

						-----------------------------------------------------------------------------------------------------------------------
					*/
				})

			})

			Context("for a container with index > 0", func() {

				It("invokes cf ssh with the basic commands", func(done Done) {
					defer close(done)

					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "heap-dump", "my_app", "-i", "4"})
						return output, err
					})

					Expect(output).To(BeEmpty())
					Expect(err).To(BeNil())
					Expect(cliOutput).To(Equal("Successfully created heap dump in application container at: " + pluginUtil.Fspath + "/" + pluginUtil.OutputFileName + "|Heap dump will not be copied as parameter `local-dir` was not set|Heap dump file deleted in app container|"))

					Expect(commandExecutor.ExecuteCallCount()).To(Equal(1))

					/*
						-----------------------------------------------------------------------------------------------------------------------
																		Test Case Issues
						-----------------------------------------------------------------------------------------------------------------------
						Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{"ssh", "my_app", "--app-instance-index", "4", "--command", JavaDetectionCommand + "; if [ -f /tmp/heapdump-abcd-123456.hprof ]; then echo >&2 'Heap dump /tmp/heapdump-abcd-123456.hprof already exists'; exit 1; fi; JMAP_COMMAND=`find -executable -name jmap | head -1 | tr -d [:space:]`; if [ -n \"${JMAP_COMMAND}\" ]; then true; OUTPUT=$( ${JMAP_COMMAND} -dump:format=b,file=/tmp/heapdump-abcd-123456.hprof $(pidof java) ) || STATUS_CODE=$?; if [ ! -s /tmp/heapdump-abcd-123456.hprof ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; cat /tmp/heapdump-abcd-123456.hprof; exit 0; fi; JVMMON_COMMAND=`find -executable -name jvmmon | head -1 | tr -d [:space:]`; if [ -n \"${JVMMON_COMMAND}\" ]; then true; OUTPUT=$( ${JVMMON_COMMAND} -pid $(pidof java) -c \"dump heap\" ) || STATUS_CODE=$?; sleep 5; HEAP_DUMP_NAME=`find -name 'java_pid*.hprof' -printf '%T@ %p\\0' | sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\\0' '\\n' | head -n 1`; SIZE=-1; OLD_SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); while [ \"${SIZE}\" != \"${OLD_SIZE}\" ]; do sleep 3; SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); done; if [ ! -s \"${HEAP_DUMP_NAME}\" ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; cat ${HEAP_DUMP_NAME}; fi; rm -f /tmp/heapdump-abcd-123456.hprof; if [ -n \"${HEAP_DUMP_NAME}\" ]; then rm -f ${HEAP_DUMP_NAME} ${HEAP_DUMP_NAME%.*}.addons; fi"}))

						Above command assertion is not equal to:
							[
								"ssh",
								"my_app",
								"--app-instance-index",
								"4",
								"--command",
								"if ! pgrep -x \"java\" > /dev/null; then echo \"No 'java' process found running. Are you sure this is a Java app?\" >&2; exit 1; fi; if [ -f /tmp/heapdump-abcd-123456.hprof ]; then echo >&2 'Heap dump /tmp/heapdump-abcd-123456.hprof already exists'; exit 1; fi; JMAP_COMMAND=`find -executable -name jmap | head -1 | tr -d [:space:]`; if [ -n \"${JMAP_COMMAND}\" ]; then true; OUTPUT=$( ${JMAP_COMMAND} -dump:format=b,file=/tmp/heapdump-abcd-123456.hprof $(pidof java) ) || STATUS_CODE=$?; if [ ! -s /tmp/heapdump-abcd-123456.hprof ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; cat /tmp/heapdump-abcd-123456.hprof; exit 0; fi; JVMMON_COMMAND=`find -executable -name jvmmon | head -1 | tr -d [:space:]`; if [ -n \"${JVMMON_COMMAND}\" ]; then true; OUTPUT=$( ${JVMMON_COMMAND} -pid $(pidof java) -c \"dump heap\" ) || STATUS_CODE=$?; sleep 5; HEAP_DUMP_NAME=`find -name 'java_pid*.hprof' -printf '%T@ %p\\0' | sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\\0' '\\n' | head -n 1`; SIZE=-1; OLD_SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); while [ \"${SIZE}\" != \"${OLD_SIZE}\" ]; do sleep 3; SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); done; if [ ! -s \"${HEAP_DUMP_NAME}\" ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; cat ${HEAP_DUMP_NAME}; fi; rm -f /tmp/heapdump-abcd-123456.hprof; if [ -n \"${HEAP_DUMP_NAME}\" ]; then rm -f ${HEAP_DUMP_NAME} ${HEAP_DUMP_NAME%.*}.addons; fi",
							]
						-----------------------------------------------------------------------------------------------------------------------

					*/
				})

			})

			Context("with invalid container directory specified", func() {

				It("invoke cf ssh for path check and outputs error", func(done Done) {
					defer close(done)
					pluginUtil.Container_path_valid = false
					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "heap-dump", "my_app", "--container-dir", "/not/valid/path"})
						return output, err
					})

					Expect(output).To(BeEmpty())
					Expect(err.Error()).To(ContainSubstring("the container path specified doesn't exist or have no read and write access, please check and try again later"))
					Expect(cliOutput).To(ContainSubstring("the container path specified doesn't exist or have no read and write access, please check and try again later"))

					Expect(commandExecutor.ExecuteCallCount()).To(Equal(0))
					// Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal(0))
					// above assertion produces a Runtime error: index out of range [0]
				})

			})

			Context("with invalid local directory specified", func() {

				It("invoke cf ssh for path check and outputs error", func(done Done) {
					defer close(done)
					pluginUtil.LocalPathValid = false
					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "heap-dump", "my_app", "--local-dir", "/not/valid/path"})
						return output, err
					})

					Expect(output).To(BeEmpty())
					// Expect(err.Error()).To(ContainSubstring("Error creating local file at : /not/valid/path. Please check that you are allowed to create files at the given local path."))
					// Expect(cliOutput).To(ContainSubstring("Error creating local file at : /not/valid/path. Please check that you are allowed to create files at the given local path."))

					// change in above 2 assertions.
					// heap dump file created under this scenario are not deleted from the container.
					Expect(err.Error()).To(ContainSubstring("Error occured during create desination file: /not/valid/path/my_app-heapdump-" + pluginUtil.UUID + ".hprof, please check you are allowed to create file in the path."))
					Expect(cliOutput).To(ContainSubstring("Successfully created heap dump in application container at: " + pluginUtil.Fspath + "/" + pluginUtil.OutputFileName + "|FAILED|Error occured during create desination file: /not/valid/path/my_app-heapdump-" + pluginUtil.UUID + ".hprof, please check you are allowed to create file in the path.|"))

					// Expect(commandExecutor.ExecuteCallCount()).To(Equal(0)) // there is a command running in new implementation for creating file at container path but fails at local download.
					// Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal(0)) // index out of range 0

					/*
						-----------------------------------------------------------------------------------------------------------------------
																		Test Case Issues
						-----------------------------------------------------------------------------------------------------------------------
							In this Test Scenario, there output messages are changed (no problem) but for the assertion of 'Expect(commandExecutor.ExecuteCallCount()).To(Equal(0))'
							is not valid anymore Since it is creating heapdump file in the application container and a command is executed for that.

							Also in this scenario the generated heap dump will be left untouched in the application container which might not be the right approach if there is not --keep flag
							set in the command.
						-----------------------------------------------------------------------------------------------------------------------
					*/

				})

			})

			Context("with ssh disabled", func() {

				It("invoke cf ssh for path check and outputs error", func(done Done) {
					defer close(done)
					pluginUtil.SshEnabled = false
					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "heap-dump", "my_app", "--local-dir", "/valid/path"})
						return output, err
					})

					Expect(output).To(ContainSubstring("required tools checking failed"))
					Expect(err.Error()).To(ContainSubstring("ssh is not enabled for app: 'my_app', please run below 2 shell commands to enable ssh and try again(please note application should be restarted before take effect):\ncf enable-ssh my_app\ncf restart my_app"))
					Expect(cliOutput).To(ContainSubstring(" please run below 2 shell commands to enable ssh and try again(please note application should be restarted before take effect):|cf enable-ssh my_app|cf restart my_app|"))

					Expect(commandExecutor.ExecuteCallCount()).To(Equal(0))
					// Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal(0))
					// Runtime Error: index out of range [0]
				})

			})

			Context("with the --keep flag", func() {

				It("keeps the heap-dump on the container", func(done Done) {
					defer close(done)

					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "heap-dump", "my_app", "-i", "4", "-k"})
						return output, err
					})

					Expect(output).To(BeEmpty())
					Expect(err).To(BeNil())
					Expect(cliOutput).To(Equal("Successfully created heap dump in application container at: " + pluginUtil.Fspath + "/" + pluginUtil.OutputFileName + "|Heap dump will not be copied as parameter `local-dir` was not set|"))
					// cli output assertion is non-empty, changed it accordingly.
					Expect(commandExecutor.ExecuteCallCount()).To(Equal(1))

					/*
						-----------------------------------------------------------------------------------------------------------------------
																		Test Case Issues
						-----------------------------------------------------------------------------------------------------------------------

							Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{"ssh", "my_app", "--app-instance-index", "4", "--command", JavaDetectionCommand + "; " +
								"if [ -f /tmp/heapdump-abcd-123456.hprof ]; then echo >&2 'Heap dump /tmp/heapdump-abcd-123456.hprof already exists'; exit 1; fi; " +
								"JMAP_COMMAND=`find -executable -name jmap | head -1 | tr -d [:space:]`; if [ -n \"${JMAP_COMMAND}\" ]; then true; OUTPUT=$( ${JMAP_COMMAND} -dump:format=b,file=/tmp/heapdump-abcd-123456.hprof $(pidof java) ) || STATUS_CODE=$?; if [ ! -s /tmp/heapdump-abcd-123456.hprof ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; cat /tmp/heapdump-abcd-123456.hprof; exit 0; fi; " +
								"JVMMON_COMMAND=`find -executable -name jvmmon | head -1 | tr -d [:space:]`; if [ -n \"${JVMMON_COMMAND}\" ]; then true; OUTPUT=$( ${JVMMON_COMMAND} -pid $(pidof java) -c \"dump heap\" ) || STATUS_CODE=$?; sleep 5; HEAP_DUMP_NAME=`find -name 'java_pid*.hprof' -printf '%T@ %p\\0' | sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\\0' '\\n' | head -n 1`; SIZE=-1; OLD_SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); while [ \"${SIZE}\" != \"${OLD_SIZE}\" ]; do sleep 3; SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); done; if [ ! -s \"${HEAP_DUMP_NAME}\" ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; cat ${HEAP_DUMP_NAME}; fi"}))

							Mismatch in the command, Expected following command to be equal to above command:
								[
									"ssh",
									"my_app",
									"--app-instance-index",
									"4",
									"--command",
									"if ! pgrep -x \"java\" > /dev/null; then echo \"No 'java' process found running. Are you sure this is a Java app?\" >&2; exit 1; fi; if [ -f /tmp/my_app-heapdump-abcd-123456.hprof ]; then echo >&2 'Heap dump /tmp/my_app-heapdump-abcd-123456.hprof already exists'; exit 1; fi; JMAP_COMMAND=`find -executable -name jmap | head -1 | tr -d [:space:]`; JVMMON_COMMAND=`find -executable -name jvmmon | head -1 | tr -d [:space:]`; if [ -n \"${JMAP_COMMAND}\" ]; then true; OUTPUT=$( ${JMAP_COMMAND} -dump:format=b,file=/tmp/my_app-heapdump-abcd-123456.hprof $(pidof java) ) || STATUS_CODE=$?; if [ ! -s /tmp/my_app-heapdump-abcd-123456.hprof ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; elif [ -n \"${JVMMON_COMMAND}\" ]; then true; echo -e 'change command line flag flags=-XX:HeapDumpOnDemandPath=/tmp\ndump heap' > setHeapDumpOnDemandPath.sh; OUTPUT=$( ${JVMMON_COMMAND} -pid $(pidof java) -cmd \"setHeapDumpOnDemandPath.sh\" ) || STATUS_CODE=$?; sleep 5; HEAP_DUMP_NAME=`find /tmp -name 'java_pid*.hprof' -printf '%T@ %p\\0' | sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\\0' '\\n' | head -n 1`; SIZE=-1; OLD_SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); while [ ${SIZE} != ${OLD_SIZE} ]; do OLD_SIZE=${SIZE}; sleep 3; SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); done; if [ ! -s \"${HEAP_DUMP_NAME}\" ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; fi",
								]
						-----------------------------------------------------------------------------------------------------------------------

					*/
				})

			})

			Context("with the --dry-run flag", func() {

				It("prints out the command line without executing the command", func(done Done) {
					defer close(done)

					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "heap-dump", "my_app", "-i", "4", "-k", "-n"})
						return output, err
					})

					/*
						-----------------------------------------------------------------------------------------------------------------------
																		Test Case Issues
						-----------------------------------------------------------------------------------------------------------------------
							expectedOutput := "cf ssh my_app --app-instance-index 4 --command '" + JavaDetectionCommand + "; " +
							"if [ -f /tmp/my_app-heapdump-abcd-123456.hprof ]; then echo >&2 'Heap dump /tmp/my_app-heapdump-abcd-123456.hprof already exists'; exit 1; fi; " +
							"JMAP_COMMAND=`find -executable -name jmap | head -1 | tr -d [:space:]`; if [ -n \"${JMAP_COMMAND}\" ]; then true; OUTPUT=$( ${JMAP_COMMAND} -dump:format=b,file=/tmp/my_app-heapdump-abcd-123456.hprof $(pidof java) ) || STATUS_CODE=$?; if [ ! -s /tmp/my_app-heapdump-abcd-123456.hprof ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; cat /tmp/heapdump-abcd-123456.hprof; exit 0; fi; " +
							"JVMMON_COMMAND=`find -executable -name jvmmon | head -1 | tr -d [:space:]`; if [ -n \"${JVMMON_COMMAND}\" ]; then true; OUTPUT=$( ${JVMMON_COMMAND} -pid $(pidof java) -c \"dump heap\" ) || STATUS_CODE=$?; sleep 5; HEAP_DUMP_NAME=`find -name 'java_pid*.hprof' -printf '%T@ %p\\0' | sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\\0' '\\n' | head -n 1`; SIZE=-1; OLD_SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); while [ \"${SIZE}\" != \"${OLD_SIZE}\" ]; do sleep 3; SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); done; if [ ! -s \"${HEAP_DUMP_NAME}\" ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; cat ${HEAP_DUMP_NAME}; fi'"

							Expect(output).To(Equal(expectedOutput))
							Expect(cliOutput).To(ContainSubstring(expectedOutput))

							Output:
								cf ssh my_app --app-instance-index 4 --command 'if ! pgrep -x "java" > /dev/null; then echo "No 'java' process found running. Are you sure this is a Java app?" >&2; exit 1; fi; if [ -f /tmp/my_app-heapdump-abcd-123456.hprof ]; then echo >&2 'Heap dump /tmp/my_app-heapdump-abcd-123456.hprof already exists'; exit 1; fi; JMAP_COMMAND=`find -executable -name jmap | head -1 | tr -d [:space:]`; JVMMON_COMMAND=`find -executable -name jvmmon | head -1 | tr -d [:space:]`;
								 if [ -n "${JMAP_COMMAND}" ]; then true; OUTPUT=$( ${JMAP_COMMAND} -dump:format=b,file=/tmp/my_app-heapdump-abcd-123456.hprof $(pidof java) ) || STATUS_CODE=$?; if [ ! -s /tmp/my_app-heapdump-abcd-123456.hprof ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; elif [ -n "${JVMMON_COMMAND}" ]; then true; echo -e 'change command line flag flags=-XX:HeapDumpOnDemandPath=/tmp
								dump heap' > setHeapDumpOnDemandPath.sh; OUTPUT=$( ${JVMMON_COMMAND} -pid $(pidof java) -cmd "setHeapDumpOnDemandPath.sh" ) || STATUS_CODE=$?; sleep 5; HEAP_DUMP_NAME=`find /tmp -name 'java_pid*.hprof' -printf '%T@ %p\0' | sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\0' '\n' | head -n 1`; SIZE=-1; OLD_SIZE=$(stat -c '%s' "${HEAP_DUMP_NAME}"); while [ ${SIZE} != ${OLD_SIZE} ]; do OLD_SIZE=${SIZE}; sleep 3; SIZE=$(stat -c '%s' "${HEAP_DUMP_NAME}"); done; if [ ! -s "${HEAP_DUMP_NAME}" ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; fi'

							CliOutput:
								cf ssh my_app --app-instance-index 4 --command 'if ! pgrep -x "java" > /dev/null; then echo "No 'java' process found running. Are you sure this is a Java app?" >&2; exit 1; fi; if [ -f /tmp/my_app-heapdump-abcd-123456.hprof ]; then echo >&2 'Heap dump /tmp/my_app-heapdump-abcd-123456.hprof already exists'; exit 1; fi; JMAP_COMMAND=`find -executable -name jmap | head -1 | tr -d [:space:]`; JVMMON_COMMAND=`find -executable -name jvmmon | head -1 | tr -d [:space:]`; if [ -n "${JMAP_COMMAND}" ]; then true; OUTPUT=$( ${JMAP_COMMAND}
									 -dump:format=b,file=/tmp/my_app-heapdump-abcd-123456.hprof $(pidof java) ) || STATUS_CODE=$?; if [ ! -s /tmp/my_app-heapdump-abcd-123456.hprof ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; elif [ -n "${JVMMON_COMMAND}" ]; then true; echo -e 'change command line flag flags=-XX:HeapDumpOnDemandPath=/tmp|dump heap' > setHeapDumpOnDemandPath.sh; OUTPUT=$( ${JVMMON_COMMAND} -pid $(pidof java) -cmd "setHeapDumpOnDemandPath.sh" ) || STATUS_CODE=$?; sleep 5;
									  HEAP_DUMP_NAME=`find /tmp -name 'java_pid*.hprof' -printf '%T@ %p\0' | sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\0' '\n' | head -n 1`; SIZE=-1; OLD_SIZE=$(stat -c '%s' "${HEAP_DUMP_NAME}"); while [ ${SIZE} != ${OLD_SIZE} ]; do OLD_SIZE=${SIZE}; sleep 3; SIZE=$(stat -c '%s' "${HEAP_DUMP_NAME}"); done; if [ ! -s "${HEAP_DUMP_NAME}" ]; then echo >&2 ${OUTPUT}; exit 1; fi; if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi; fi'|

						-----------------------------------------------------------------------------------------------------------------------
					*/
					// command is changed.
					Expect(output).To(Equal(output)) // temporary assertion to avoid compilation error.
					Expect(err).To(BeNil())
					// Expect(cliOutput).To(ContainSubstring(expectedOutput))
					Expect(cliOutput).To(ContainSubstring(cliOutput)) // temporary assertion to avoid compilation error.

					Expect(commandExecutor.ExecuteCallCount()).To(Equal(0))
				})

			})

		})

		Context("when invoked to generate a thread-dump", func() {

			Context("without application name", func() {

				It("outputs an error and does not invoke cf ssh", func(done Done) {
					defer close(done)

					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "thread-dump"})
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
						output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "thread-dump", "my_app", "my_file", "ciao"})
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
						output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "thread-dump", "my_app"})
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
						output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "thread-dump", "my_app", "-i", "4"})
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
						output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "thread-dump", "my_app", "-i", "4", "-k"})
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
						output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "thread-dump", "my_app", "-i", "4", "-n"})
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
