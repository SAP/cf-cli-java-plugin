package fakes

import (
	"errors"
	"strings"
)

type FakeCfJavaPluginUtil struct {
	SshEnabled           bool
	Jmap_jvmmon_present  bool
	Container_path_valid bool
	Fspath               string
	LocalPathValid       bool
	UUID                 string
	OutputFileName       string
}

func (fakeUtil FakeCfJavaPluginUtil) CheckRequiredTools(app string) (bool, error) {

	if !fakeUtil.SshEnabled {
		return false, errors.New("ssh is not enabled for app: '" + app + "', please run below 2 shell commands to enable ssh and try again(please note application should be restarted before take effect):\ncf enable-ssh " + app + "\ncf restart " + app)
	}

	if !fakeUtil.Jmap_jvmmon_present {
		return false, errors.New(`jvmmon or jmap are required for generating heap dump, you can modify your application manifest.yaml on the 'JBP_CONFIG_OPEN_JDK_JRE' environment variable. This could be done like this:
		---
		applications:
		- name: <APP_NAME>
		  memory: 1G
		  path: <PATH_TO_BUILD_ARTIFACT>
		  buildpack: https://github.com/cloudfoundry/java-buildpack
		  env:
			JBP_CONFIG_OPEN_JDK_JRE: '{ jre: { repository_root: "https://java-buildpack.cloudfoundry.org/openjdk-jdk/bionic/x86_64", version: 11.+ } }'
		
		`)
	}

	return true, nil
}

func (fake FakeCfJavaPluginUtil) GetAvailablePath(data string, userpath string) (string, error) {
	if !fake.Container_path_valid && len(userpath) > 0 {
		return "", errors.New("the container path specified doesn't exist or have no read and write access, please check and try again later")
	}

	if len(fake.Fspath) > 0 {
		return fake.Fspath, nil
	}

	return "/tmp", nil
}

func (fake FakeCfJavaPluginUtil) CopyOverCat(args []string, src string, dest string) error {

	if !fake.LocalPathValid {
		return errors.New("Error occured during create desination file: " + dest + ", please check you are allowed to create file in the path.")
	}

	return nil
}

func (fake FakeCfJavaPluginUtil) DeleteRemoteFile(args []string, path string) error {
	if path != fake.Fspath+"/"+fake.OutputFileName {
		return errors.New("error occured while removing dump file generated")
	}

	return nil
}

func (fake FakeCfJavaPluginUtil) FindFakeFile(args []string, fullpath string, fspath string, expectedFullPath string) (string, error) {
	if fspath != fake.Fspath || fullpath != expectedFullPath {
		return "", errors.New("error while checking the generated file")
	}
	output := fspath + "/" + fake.OutputFileName

	return strings.Trim(string(output[:]), "\n"), nil
}

func (fake FakeCfJavaPluginUtil) FindHeapDumpFile(args []string, fullpath string, fspath string) (string, error) {

	expectedFullPath := fake.Fspath + "/" + args[1] + "-heapdump-" + fake.UUID + ".hprof"
	return fake.FindFakeFile(args, fullpath, fspath, expectedFullPath)
}

func (fake FakeCfJavaPluginUtil) FindJFRFile(args []string, fullpath string, fspath string) (string, error) {

	expectedFullPath := fake.Fspath + "/" + args[1] + "-profile-" + fake.UUID + ".jfr"
	return fake.FindFakeFile(args, fullpath, fspath, expectedFullPath)
}


func (fake FakeCfJavaPluginUtil) FindFile(args []string, fullpath string, fspath string, pattern string) (string, error) {
	return fake.FindHeapDumpFile(args, fullpath, fspath) // same as FindHeapDumpFile, just to avoid duplication
}

func (fake FakeCfJavaPluginUtil) ListFiles(args []string, path string) ([]string, error) {
	return []string{fake.OutputFileName}, nil
}