package utils

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/go-yaml/yaml"
)

	

type CFAppEnv struct {
	SystemProvided interface{} `yaml:"System-Provided"`
	VCAPSERVICES struct {
		FsStorage []struct {
			BindingGUID string `yaml:"binding_guid"`
			BindingName interface{} `yaml:"binding_name"`
			Credentials struct {
			} `yaml:"credentials"`
			InstanceGUID string `yaml:"instance_guid"`
			InstanceName string `yaml:"instance_name"`
			Label string `yaml:"label"`
			Name string `yaml:"name"`
			Plan string `yaml:"plan"`
			Provider interface{} `yaml:"provider"`
			SyslogDrainURL interface{} `yaml:"syslog_drain_url"`
			Tags []string `yaml:"tags"`
			VolumeMounts []struct {
				ContainerDir string `yaml:"container_dir"`
				DeviceType string `yaml:"device_type"`
				Mode string `yaml:"mode"`
			} `yaml:"volume_mounts"`
		} `yaml:"fs-storage"`
	} `yaml:"VCAP_SERVICES"`
	VCAPAPPLICATION struct {
		ApplicationID string `yaml:"application_id"`
		ApplicationName string `yaml:"application_name"`
		ApplicationUris []string `yaml:"application_uris"`
		CfAPI string `yaml:"cf_api"`
		Limits struct {
			Fds int `yaml:"fds"`
		} `yaml:"limits"`
		Name string `yaml:"name"`
		OrganizationID string `yaml:"organization_id"`
		OrganizationName string `yaml:"organization_name"`
		SpaceID string `yaml:"space_id"`
		SpaceName string `yaml:"space_name"`
		Uris []string `yaml:"uris"`
		Users interface{} `yaml:"users"`
	} `yaml:"VCAP_APPLICATION"`
	UserProvided interface{} `yaml:"User-Provided"`
	JBPCONFIGOPENJDKJRE struct {
		Jre struct {
			Version string `yaml:"version"`
		} `yaml:"jre"`
	} `yaml:"JBP_CONFIG_OPEN_JDK_JRE"`
	JBPCONFIGSPRINGAUTORECONFIGURATION struct {
		Enabled bool `yaml:"enabled"`
	} `yaml:"JBP_CONFIG_SPRING_AUTO_RECONFIGURATION"`
	RunningEnvironmentVariableGroups interface{} `yaml:"Running Environment Variable Groups"`
	CREDHUBAPI string `yaml:"CREDHUB_API"`
}

func readAppEnv(app string) ([]byte, error){
	env, err := exec.Command("cf", "env", app).Output()

	if err != nil{
		return nil, err
	}

	noises := []string { "No staging env variables have been set",
	}

	res := string(env[:])
	for _,v := range noises{
		res = strings.ReplaceAll(res, v, "")
	}
	res = res[:strings.Index(res, "User-Provided:")]

	val := strings.Split(res, "\n")
	for i,v := range val{
		if i ==0 {
			res = ""
			continue
		}
		res = res + "\n" + v
	}
	return []byte(res), nil
}

func checkUserPathAvailability(app string, path string)(bool, error){
	//fmt.Println("[[ -d \"" + path + "\" && -r \"" + path + "\" && -w \"" + path + "\" ]] && echo \"exists and read-writeable\"")
	output, err := exec.Command("cf", "ssh", app, "-c", "[[ -d \"" + path + "\" && -r \"" + path + "\" && -w \"" + path + "\" ]] && echo \"exists and read-writeable\"").Output()
	if err!= nil{
		return false, err
	}

	if strings.Contains(string(output[:]), "exists and read-writeable"){
		return true, nil
	}

	return false, nil
}

func CheckRequiredTools(app string)(bool, error){
	output, err := exec.Command("cf", "ssh-enabled", app).Output()
	if err != nil{
		return false, err
	}
	if !strings.Contains(string(output[:]), "ssh support is enabled for app '" + app + "'."){
		fmt.Fprintln(os.Stderr,"ssh is not enabled for app: '" + app + "', please run below 2 shell commands to enable ssh and try again(please note application should be restarted before take effect):\ncf enable-ssh " + app + "\ncf restart "+app)
		return false, nil
	}

	output, err = exec.Command("cf", "ssh", app, "-c", "find -executable -name jvmmon | head -1 | tr -d [:space:] | find -executable -name jmap | head -1 | tr -d [:space:]").Output()
	if err != nil{
		fmt.Fprintln(os.Stderr,"unknown error occured while checking existence of required tools jvmmon/jmap")
		return false, nil
	}
	if !strings.Contains(string(output[:]), "/"){
		fmt.Fprintln(os.Stderr,`jvmmon or jmap are required for generating heap dump, you can modify your application manifest.yaml on the 'JBP_CONFIG_OPEN_JDK_JRE' environment variable. This could be done like this:
		---
		applications:
		- name: <APP_NAME>
		  memory: 1G
		  path: <PATH_TO_BUILD_ARTIFACT>
		  buildpack: https://github.com/cloudfoundry/java-buildpack
		  env:
			JBP_CONFIG_OPEN_JDK_JRE: '{ jre: { repository_root: "https://java-buildpack.cloudfoundry.org/openjdk-jdk/bionic/x86_64", version: 11.+ } }'
		
		`)

		return false, nil
	}

	return true, nil
}

func GetAvailablePath(data string, userpath string)(string, error){
	valid, _ := checkUserPathAvailability(data, userpath)

	if valid {
		//fmt.Println("userpath '" + userpath + "' will be used")
		return userpath, nil
	}

	env, err := readAppEnv(data)
	if err != nil{
		return "/tmp", nil
	}

	//fmt.Println(string(env[:]))

	var cfAppEnv CFAppEnv
	yaml.Unmarshal(env, &cfAppEnv)

	//fmt.Println(result)
	
	for _,v := range cfAppEnv.VCAPSERVICES.FsStorage{
		for _,v2 := range v.VolumeMounts{
			if v2.Mode=="rw" {
				return v2.ContainerDir, nil
			}
		}
	}
	
	return "/tmp", nil
}

func CopyOverCat(app string, src string, dest string ) error{
	f, err := os.OpenFile(dest,os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err!=nil{
		fmt.Fprintln(os.Stderr, "error occured during create desination file: " + dest + ", please check you are allowed to create file in the path.")
		return err
	}
	defer f.Close()

	cat := exec.Command("cf", "ssh", app, "-c", "cat " + src )
	
	cat.Stdout = f
	
	err = cat.Start()
	if err!=nil{
		fmt.Fprintln(os.Stderr, "error occured during copying dump file: " + src + ", please try again.")
		return err
	}

	err = cat.Wait()
	if err != nil{
		fmt.Fprintln(os.Stderr, "error occured while waiting for the copying complete.")
		return err
	}

	return nil
}

func DeleteRemoteFile(app string, path string) error{
	_, err := exec.Command("cf", "ssh", app, "-c", "rm " + path ).Output()

	if err != nil{
		fmt.Fprintln(os.Stderr, "error occured while removing dump file generated: %V", err)
		return err
	}

	return nil
}

func FindDumpFile(app string, path string) (string, error){
	cmd := " [ -f '" + path +"' ] && echo '" + path + "' ||  find -name 'java_pid*.hprof' -printf '%T@ %p\\0' | sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\\0' '\\n' | head -n 1  "

	output, err := exec.Command("cf", "ssh", app, "-c", cmd).Output()
	
	if err != nil{
		fmt.Fprintln(os.Stderr, "error occured while checking the generated file: %V", err)
		return "", err
	}

	return strings.Trim(string(output[:]), "\n"), nil

}