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
			uuidGenerator.GenerateReturns("cdc8cea3-92e6-4f92-8dc7-c4952dd67be5")
			pluginUtil = FakeCfJavaPluginUtil{SshEnabled: true, Jmap_jvmmon_present: true, Container_path_valid: true, Fspath: "/tmp", LocalPathValid: true, UUID: uuidGenerator.Generate(), OutputFileName: "java_pid0_0.hprof"}
		})

		Context("when invoked without arguments", func() {

			It("outputs an error and does not invoke cf ssh", func() {

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

			It("outputs an error and does not invoke cf ssh", func() {

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

			It("outputs an error and does not invoke cf ssh", func() {

				output, err, cliOutput := captureOutput(func() (string, error) {
					output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "UNKNOWN_COMMAND"})
					return output, err
				})

				Expect(output).To(BeEmpty())
				Expect(err.Error()).To(ContainSubstring("Unrecognized command \"UNKNOWN_COMMAND\": supported commands"))
				Expect(cliOutput).To(ContainSubstring("Unrecognized command \"UNKNOWN_COMMAND\": supported commands"))

				Expect(commandExecutor.ExecuteCallCount()).To(Equal(1))
				Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{"help", "java"}))
			})

		})

		Context("when invoked to generate a heap-dump", func() {

			Context("without application name", func() {

				It("outputs an error and does not invoke cf ssh", func() {

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

				It("outputs an error and does not invoke cf ssh", func() {

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

				It("invokes cf ssh with the basic commands", func() {

					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "heap-dump", "my_app"})
						return output, err
					})
					Expect(output).To(BeEmpty())
					Expect(err).To(BeNil())
					Expect(cliOutput).To(Equal("Successfully created heap dump in application container at: " + pluginUtil.Fspath + "/" + pluginUtil.OutputFileName + "|Heap dump will not be copied as parameter `local-dir` was not set|Heap dump file deleted in app container|"))

					Expect(commandExecutor.ExecuteCallCount()).To(Equal(1))
					Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{"ssh",
						"my_app",
						"--command",
						"if ! pgrep -x \"java\" > /dev/null; then echo \"No 'java' process found running. Are you sure this is a Java app?\" >&2; exit 1; fi; if [ -f /tmp/my_app-heapdump-" + pluginUtil.UUID + ".hprof ]; then echo >&2 'Heap dump /tmp/my_app-heapdump-" + pluginUtil.UUID + ".hprof already exists'; exit 1; fi\nJMAP_COMMAND=$(find -executable -name jmap | head -1 | tr -d [:space:])\n# SAP JVM: Wrap everything in an if statement in case jvmmon is available\nJVMMON_COMMAND=$(find -executable -name jvmmon | head -1 | tr -d [:space:])\nif [ -n \"${JMAP_COMMAND}\" ]; then true\nOUTPUT=$( ${JMAP_COMMAND} -dump:format=b,file=/tmp/my_app-heapdump-" + pluginUtil.UUID + ".hprof $(pidof java) ) || STATUS_CODE=$?\nif [ ! -s /tmp/my_app-heapdump-" + pluginUtil.UUID + ".hprof ]; then echo >&2 ${OUTPUT}; exit 1; fi\nif [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi\nelif [ -n \"${JVMMON_COMMAND}\" ]; then true\necho -e 'change command line flag flags=-XX:HeapDumpOnDemandPath=/tmp\\ndump heap' > setHeapDumpOnDemandPath.sh\nOUTPUT=$( ${JVMMON_COMMAND} -pid $(pidof java) -cmd \"setHeapDumpOnDemandPath.sh\" ) || STATUS_CODE=$?\nsleep 5 # Writing the heap dump is triggered asynchronously -> give the JVM some time to create the file\nHEAP_DUMP_NAME=$(find /tmp -name 'java_pid*.hprof' -printf '%T@ %p\\0' | sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\\0' '\\n' | head -n 1)\nSIZE=-1; OLD_SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); while [ ${SIZE} != ${OLD_SIZE} ]; do OLD_SIZE=${SIZE}; sleep 3; SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); done\nif [ ! -s \"${HEAP_DUMP_NAME}\" ]; then echo >&2 ${OUTPUT}; exit 1; fi\nif [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi\nfi",
					}))

				})

			})

			Context("for a container with index > 0", func() {

				It("invokes cf ssh with the basic commands", func() {

					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "heap-dump", "my_app", "-i", "4"})
						return output, err
					})

					Expect(output).To(BeEmpty())
					Expect(err).To(BeNil())
					Expect(cliOutput).To(Equal("Successfully created heap dump in application container at: " + pluginUtil.Fspath + "/" + pluginUtil.OutputFileName + "|Heap dump will not be copied as parameter `local-dir` was not set|Heap dump file deleted in app container|"))

					Expect(commandExecutor.ExecuteCallCount()).To(Equal(1))
					Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{
						"ssh",
						"my_app",
						"--app-instance-index",
						"4",
						"--command",
						"if ! pgrep -x \"java\" > /dev/null; then echo \"No 'java' process found running. Are you sure this is a Java app?\" >&2; exit 1; fi; if [ -f /tmp/my_app-heapdump-" + pluginUtil.UUID + ".hprof ]; then echo >&2 'Heap dump /tmp/my_app-heapdump-" + pluginUtil.UUID + ".hprof already exists'; exit 1; fi\nJMAP_COMMAND=$(find -executable -name jmap | head -1 | tr -d [:space:])\n# SAP JVM: Wrap everything in an if statement in case jvmmon is available\nJVMMON_COMMAND=$(find -executable -name jvmmon | head -1 | tr -d [:space:])\nif [ -n \"${JMAP_COMMAND}\" ]; then true\nOUTPUT=$( ${JMAP_COMMAND} -dump:format=b,file=/tmp/my_app-heapdump-" + pluginUtil.UUID + ".hprof $(pidof java) ) || STATUS_CODE=$?\nif [ ! -s /tmp/my_app-heapdump-" + pluginUtil.UUID + ".hprof ]; then echo >&2 ${OUTPUT}; exit 1; fi\nif [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi\nelif [ -n \"${JVMMON_COMMAND}\" ]; then true\necho -e 'change command line flag flags=-XX:HeapDumpOnDemandPath=/tmp\\ndump heap' > setHeapDumpOnDemandPath.sh\nOUTPUT=$( ${JVMMON_COMMAND} -pid $(pidof java) -cmd \"setHeapDumpOnDemandPath.sh\" ) || STATUS_CODE=$?\nsleep 5 # Writing the heap dump is triggered asynchronously -> give the JVM some time to create the file\nHEAP_DUMP_NAME=$(find /tmp -name 'java_pid*.hprof' -printf '%T@ %p\\0' | sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\\0' '\\n' | head -n 1)\nSIZE=-1; OLD_SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); while [ ${SIZE} != ${OLD_SIZE} ]; do OLD_SIZE=${SIZE}; sleep 3; SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); done\nif [ ! -s \"${HEAP_DUMP_NAME}\" ]; then echo >&2 ${OUTPUT}; exit 1; fi\nif [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi\nfi"}))

				})

			})

			Context("with invalid container directory specified", func() {

				It("invoke cf ssh for path check and outputs error", func() {
					pluginUtil.Container_path_valid = false
					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "heap-dump", "my_app", "--container-dir", "/not/valid/path"})
						return output, err
					})

					Expect(output).To(BeEmpty())
					Expect(err.Error()).To(ContainSubstring("the container path specified doesn't exist or have no read and write access, please check and try again later"))
					Expect(cliOutput).To(ContainSubstring("the container path specified doesn't exist or have no read and write access, please check and try again later"))

					Expect(commandExecutor.ExecuteCallCount()).To(Equal(0))

				})

			})

			Context("with invalid local directory specified", func() {

				It("invoke cf ssh for path check and outputs error", func() {
					pluginUtil.LocalPathValid = false
					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "heap-dump", "my_app", "--local-dir", "/not/valid/path"})
						return output, err
					})

					Expect(output).To(BeEmpty())
					Expect(err.Error()).To(ContainSubstring("Error occured during create desination file: /not/valid/path/my_app-heapdump-" + pluginUtil.UUID + ".hprof, please check you are allowed to create file in the path."))
					Expect(cliOutput).To(ContainSubstring("Successfully created heap dump in application container at: " + pluginUtil.Fspath + "/" + pluginUtil.OutputFileName + "|FAILED|Error occured during create desination file: /not/valid/path/my_app-heapdump-" + pluginUtil.UUID + ".hprof, please check you are allowed to create file in the path.|"))

					Expect(commandExecutor.ExecuteCallCount()).To(Equal(1))

				})

			})

			Context("with ssh disabled", func() {

				It("invoke cf ssh for path check and outputs error", func() {
					pluginUtil.SshEnabled = false
					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "heap-dump", "my_app", "--local-dir", "/valid/path"})
						return output, err
					})

					Expect(output).To(ContainSubstring("required tools checking failed"))
					Expect(err.Error()).To(ContainSubstring("ssh is not enabled for app: 'my_app', please run below 2 shell commands to enable ssh and try again(please note application should be restarted before take effect):\ncf enable-ssh my_app\ncf restart my_app"))
					Expect(cliOutput).To(ContainSubstring(" please run below 2 shell commands to enable ssh and try again(please note application should be restarted before take effect):|cf enable-ssh my_app|cf restart my_app|"))

					Expect(commandExecutor.ExecuteCallCount()).To(Equal(0))

				})

			})

			Context("with the --keep flag", func() {

				It("keeps the heap-dump on the container", func() {

					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "heap-dump", "my_app", "-i", "4", "-k"})
						return output, err
					})

					Expect(output).To(BeEmpty())
					Expect(err).To(BeNil())
					Expect(cliOutput).To(Equal("Successfully created heap dump in application container at: " + pluginUtil.Fspath + "/" + pluginUtil.OutputFileName + "|Heap dump will not be copied as parameter `local-dir` was not set|"))
					Expect(commandExecutor.ExecuteCallCount()).To(Equal(1))
					Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{"ssh",
						"my_app",
						"--app-instance-index",
						"4",
						"--command",
						"if ! pgrep -x \"java\" > /dev/null; then echo \"No 'java' process found running. Are you sure this is a Java app?\" >&2; exit 1; fi; if [ -f /tmp/my_app-heapdump-" + pluginUtil.UUID + ".hprof ]; then echo >&2 'Heap dump /tmp/my_app-heapdump-" + pluginUtil.UUID + ".hprof already exists'; exit 1; fi\nJMAP_COMMAND=$(find -executable -name jmap | head -1 | tr -d [:space:])\n# SAP JVM: Wrap everything in an if statement in case jvmmon is available\nJVMMON_COMMAND=$(find -executable -name jvmmon | head -1 | tr -d [:space:])\nif [ -n \"${JMAP_COMMAND}\" ]; then true\nOUTPUT=$( ${JMAP_COMMAND} -dump:format=b,file=/tmp/my_app-heapdump-" + pluginUtil.UUID + ".hprof $(pidof java) ) || STATUS_CODE=$?\nif [ ! -s /tmp/my_app-heapdump-" + pluginUtil.UUID + ".hprof ]; then echo >&2 ${OUTPUT}; exit 1; fi\nif [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi\nelif [ -n \"${JVMMON_COMMAND}\" ]; then true\necho -e 'change command line flag flags=-XX:HeapDumpOnDemandPath=/tmp\\ndump heap' > setHeapDumpOnDemandPath.sh\nOUTPUT=$( ${JVMMON_COMMAND} -pid $(pidof java) -cmd \"setHeapDumpOnDemandPath.sh\" ) || STATUS_CODE=$?\nsleep 5 # Writing the heap dump is triggered asynchronously -> give the JVM some time to create the file\nHEAP_DUMP_NAME=$(find /tmp -name 'java_pid*.hprof' -printf '%T@ %p\\0' | sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\\0' '\\n' | head -n 1)\nSIZE=-1; OLD_SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); while [ ${SIZE} != ${OLD_SIZE} ]; do OLD_SIZE=${SIZE}; sleep 3; SIZE=$(stat -c '%s' \"${HEAP_DUMP_NAME}\"); done\nif [ ! -s \"${HEAP_DUMP_NAME}\" ]; then echo >&2 ${OUTPUT}; exit 1; fi\nif [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi\nfi"}))

				})

			})

			Context("with the --dry-run flag", func() {

				It("prints out the command line without executing the command", func() {

					output, err, _ := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "heap-dump", "my_app", "-i", "4", "-k", "-n"})
						return output, err
					})

					expectedOutput := strings.ReplaceAll(`cf ssh my_app --app-instance-index 4 --command 'if ! pgrep -x "java" > /dev/null; then echo "No 'java' process found running. Are you sure this is a Java app?" >&2; exit 1; fi; if [ -f /tmp/my_app-heapdump-UUUID.hprof ]; then echo >&2 'Heap dump /tmp/my_app-heapdump-UUUID.hprof already exists'; exit 1; fi
JMAP_COMMAND=$(find -executable -name jmap | head -1 | tr -d [:space:])
# SAP JVM: Wrap everything in an if statement in case jvmmon is available
JVMMON_COMMAND=$(find -executable -name jvmmon | head -1 | tr -d [:space:])
if [ -n "${JMAP_COMMAND}" ]; then true
OUTPUT=$( ${JMAP_COMMAND} -dump:format=b,file=/tmp/my_app-heapdump-UUUID.hprof $(pidof java) ) || STATUS_CODE=$?
if [ ! -s /tmp/my_app-heapdump-UUUID.hprof ]; then echo >&2 ${OUTPUT}; exit 1; fi
if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi
elif [ -n "${JVMMON_COMMAND}" ]; then true
echo -e 'change command line flag flags=-XX:HeapDumpOnDemandPath=/tmp\ndump heap' > setHeapDumpOnDemandPath.sh
OUTPUT=$( ${JVMMON_COMMAND} -pid $(pidof java) -cmd "setHeapDumpOnDemandPath.sh" ) || STATUS_CODE=$?
sleep 5 # Writing the heap dump is triggered asynchronously -> give the JVM some time to create the file
HEAP_DUMP_NAME=$(find /tmp -name 'java_pid*.hprof' -printf '%T@ %p\0' | sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\0' '\n' | head -n 1)
SIZE=-1; OLD_SIZE=$(stat -c '%s' "${HEAP_DUMP_NAME}"); while [ ${SIZE} != ${OLD_SIZE} ]; do OLD_SIZE=${SIZE}; sleep 3; SIZE=$(stat -c '%s' "${HEAP_DUMP_NAME}"); done
if [ ! -s "${HEAP_DUMP_NAME}" ]; then echo >&2 ${OUTPUT}; exit 1; fi
if [ ${STATUS_CODE:-0} -gt 0 ]; then echo >&2 ${OUTPUT}; exit ${STATUS_CODE}; fi
fi'`, "UUUID", pluginUtil.UUID)

					Expect(output).To(Equal(expectedOutput))

					Expect(err).To(BeNil())
					Expect(commandExecutor.ExecuteCallCount()).To(Equal(0))
				})

			})

		})

		Context("when invoked to generate a thread-dump", func() {

			Context("without application name", func() {

				It("outputs an error and does not invoke cf ssh", func() {

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

				It("outputs an error and does not invoke cf ssh", func() {

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

				It("invokes cf ssh with the basic commands", func() {

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

				It("invokes cf ssh with the basic commands", func() {

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

				It("fails", func() {

					output, err, cliOutput := captureOutput(func() (string, error) {
						output, err := subject.DoRun(commandExecutor, uuidGenerator, pluginUtil, []string{"java", "thread-dump", "my_app", "-i", "4", "-k"})
						return output, err
					})

					Expect(output).To(BeEmpty())
					Expect(err.Error()).To(ContainSubstring("The flag \"keep\" is not supported for thread-dump"))
					Expect(cliOutput).To(ContainSubstring("The flag \"keep\" is not supported for thread-dump"))

					Expect(commandExecutor.ExecuteCallCount()).To(Equal(1))
					Expect(commandExecutor.ExecuteArgsForCall(0)).To(Equal([]string{"help", "java"}))
				})

			})

			Context("with the --dry-run flag", func() {

				It("prints out the command line without executing the command", func() {

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
