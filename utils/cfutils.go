package utils

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"slices"
)

type CfJavaPluginUtilImpl struct {
}

type CFAppEnv struct {
	EnvironmentVariables struct {
		JbpConfigSpringAutoReconfiguration string `json:"JBP_CONFIG_SPRING_AUTO_RECONFIGURATION"`
		JbpConfigOpenJdkJre                string `json:"JBP_CONFIG_OPEN_JDK_JRE"`
		JbpConfigComponents                string `json:"JBP_CONFIG_COMPONENTS"`
	} `json:"environment_variables"`
	StagingEnvJSON struct {
	} `json:"staging_env_json"`
	RunningEnvJSON struct {
		CredhubAPI string `json:"CREDHUB_API"`
	} `json:"running_env_json"`
	SystemEnvJSON struct {
		VcapServices struct {
			FsStorage []struct {
				Label        string      `json:"label"`
				Provider     interface{} `json:"provider"`
				Plan         string      `json:"plan"`
				Name         string      `json:"name"`
				Tags         []string    `json:"tags"`
				InstanceGUID string      `json:"instance_guid"`
				InstanceName string      `json:"instance_name"`
				BindingGUID  string      `json:"binding_guid"`
				BindingName  interface{} `json:"binding_name"`
				Credentials  struct {
				} `json:"credentials"`
				SyslogDrainURL interface{} `json:"syslog_drain_url"`
				VolumeMounts   []struct {
					ContainerDir string `json:"container_dir"`
					Mode         string `json:"mode"`
					DeviceType   string `json:"device_type"`
				} `json:"volume_mounts"`
			} `json:"fs-storage"`
		} `json:"VCAP_SERVICES"`
	} `json:"system_env_json"`
	ApplicationEnvJSON struct {
		VcapApplication struct {
			CfAPI  string `json:"cf_api"`
			Limits struct {
				Fds int `json:"fds"`
			} `json:"limits"`
			ApplicationName  string      `json:"application_name"`
			ApplicationUris  []string    `json:"application_uris"`
			Name             string      `json:"name"`
			SpaceName        string      `json:"space_name"`
			SpaceID          string      `json:"space_id"`
			OrganizationID   string      `json:"organization_id"`
			OrganizationName string      `json:"organization_name"`
			Uris             []string    `json:"uris"`
			Users            interface{} `json:"users"`
			ApplicationID    string      `json:"application_id"`
		} `json:"VCAP_APPLICATION"`
	} `json:"application_env_json"`
}

func readAppEnv(app string) ([]byte, error) {
	guid, err := exec.Command("cf", "app", app, "--guid").Output()
	if err != nil {
		return nil, err
	}

	env, err := exec.Command("cf", "curl", fmt.Sprintf("/v3/apps/%s/env", strings.Trim(string(guid[:]), "\n"))).Output()
	if err != nil {
		return nil, err
	}
	return env, nil

}

func checkUserPathAvailability(app string, path string) (bool, error) {
	output, err := exec.Command("cf", "ssh", app, "-c", "[[ -d \""+path+"\" && -r \""+path+"\" && -w \""+path+"\" ]] && echo \"exists and read-writeable\"").Output()
	if err != nil {
		return false, err
	}

	if strings.Contains(string(output[:]), "exists and read-writeable") {
		return true, nil
	}

	return false, nil
}

func (checker CfJavaPluginUtilImpl) FindReasonForAccessError(app string) string {
	out, err := exec.Command("cf", "apps").Output()
	if err != nil {
		return "cf is not logged in, please login and try again"
	}
	// find all app names
	lines := strings.Split(string(out[:]), "\n")
	appNames := make([]string, 100)
	foundHeader := false
	for _, line := range lines {
		if foundHeader && len(line) > 0 {
			appNames = append(appNames, strings.Fields(line)[0])
		} else if strings.Contains(line, "name") && strings.Contains(line, "requested state") && strings.Contains(line, "processes") && strings.Contains(line, "routes") {
			foundHeader = true
		}
	}
	if len(appNames) == 0 {
		return "No apps in your realm, please check if you're logged in and the app exists"
	}
	if slices.Contains(appNames, app) {
		return "Problems accessing the app " + app
	}
	matches := FuzzySearch(app, appNames, 1)
	return "Could not find " + app + ". Did you mean " + matches[0] + "?"
}
func (checker CfJavaPluginUtilImpl) CheckRequiredTools(app string) (bool, error) {
	guid, err := exec.Command("cf", "app", app, "--guid").Output()
	if err != nil {
		return false, errors.New(checker.FindReasonForAccessError(app))
	}
	output, err := exec.Command("cf", "curl", "/v3/apps/"+strings.TrimSuffix(string(guid), "\n")+"/ssh_enabled").Output()
	if err != nil {
		return false, err
	}
	var result map[string]interface{}
	json.Unmarshal([]byte(output), &result)

	if enabled, ok := result["enabled"].(bool); !ok || !enabled {
		return false, errors.New("ssh is not enabled for app: '" + app + "', please run below 2 shell commands to enable ssh and try again(please note application should be restarted before take effect):\ncf enable-ssh " + app + "\ncf restart " + app)
	}

	output, err = exec.Command("cf", "ssh", app, "-c", "find -executable | grep -E '(.*jmap$)|(.*jvmmon$)'").Output()
	if err != nil {
		return false, errors.New("unknown error occured while checking existence of required tools jvmmon/jmap")

	}
	if !strings.Contains(string(output[:]), "/") {
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

func (checker CfJavaPluginUtilImpl) GetAvailablePath(data string, userpath string) (string, error) {
	if len(userpath) > 0 {
		valid, _ := checkUserPathAvailability(data, userpath)
		if valid {
			return userpath, nil
		}

		return "", errors.New("the container path specified doesn't exist or have no read and write access, please check and try again later")
	}

	env, err := readAppEnv(data)
	if err != nil {
		return "/tmp", nil
	}

	var cfAppEnv CFAppEnv
	json.Unmarshal(env, &cfAppEnv)

	for _, v := range cfAppEnv.SystemEnvJSON.VcapServices.FsStorage {
		for _, v2 := range v.VolumeMounts {
			if v2.Mode == "rw" {
				return v2.ContainerDir, nil
			}
		}
	}

	return "/tmp", nil
}

func (checker CfJavaPluginUtilImpl) CopyOverCat(args []string, src string, dest string) error {
	fmt.Println("Copying file from remote to local: ", src, " to ", dest)
	fmt.Println("Args used: ", args)
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return errors.New("Error creating local file at  " + dest + ". Please check that you are allowed to create files at the given local path.")
	}
	defer f.Close()

	args = append(args, "cat "+src)
	cat := exec.Command("cf", args...)

	cat.Stdout = f

	err = cat.Start()
	if err != nil {
		return errors.New("error occured during copying dump file: " + src + ", please try again.")
	}

	err = cat.Wait()
	if err != nil {
		return errors.New("error occured while waiting for the copying complete")
	}

	return nil
}

func (checker CfJavaPluginUtilImpl) DeleteRemoteFile(args []string, path string) error {
	args = append(args, "rm -fr "+path)
	_, err := exec.Command("cf", args...).Output()

	if err != nil {
		return errors.New("error occured while removing dump file generated")

	}

	return nil
}

func (checker CfJavaPluginUtilImpl) FindHeapDumpFile(args []string, fullpath string, fspath string) (string, error) {
	return checker.FindFile(args, fullpath, fspath, "java_pid*.hprof")
}

func (checker CfJavaPluginUtilImpl) FindJFRFile(args []string, fullpath string, fspath string) (string, error) {
	return checker.FindFile(args, fullpath, fspath, "*.jfr")
}

func (checker CfJavaPluginUtilImpl) FindFile(args []string, fullpath string, fspath string, pattern string) (string, error) {
	cmd := " [ -f '" + fullpath + "' ] && echo '" + fullpath + "' ||  find " + fspath + " -name '" + pattern + "' -printf '%T@ %p\\0' | sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\\0' '\\n' | head -n 1 "

	args = append(args, cmd)
	output, err := exec.Command("cf", args...).Output()

	if err != nil {
		return "", errors.New("error while checking the generated file")
	}

	return strings.Trim(string(output[:]), "\n"), nil

}

func (checker CfJavaPluginUtilImpl) ListFiles(args []string, path string) ([]string, error) {
	cmd := "ls " + path
	args = append(args, cmd)
	output, err := exec.Command("cf", args...).Output()

	if err != nil {
		return nil, errors.New("error occured while listing files: " + string(output[:]))
	}
	files := strings.Split(strings.Trim(string(output[:]), "\n"), "\n")
	// filter all empty strings
	for i := 0; i < len(files); i++ {
		if len(files[i]) == 0 {
			files = append(files[:i], files[i+1:]...)
			i--
		}
	}
	return files, nil
}
