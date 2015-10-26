package builders

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var multispaces = regexp.MustCompile(`\s+`)

// GoDeps calls go get for specific package
func GoDeps(targetdir string) error {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("godeps.Error: %+s", err)
		}
	}()

	cmdline := []string{"go", "get"}

	cmdline = append(cmdline, targetdir)

	//setup the executor and use a shard buffer
	cmd := exec.Command("go", cmdline[1:]...)
	buf := bytes.NewBuffer([]byte{})
	cmd.Stdout = buf
	cmd.Stderr = buf

	err := cmd.Run()

	if buf.Len() > 0 {
		return fmt.Errorf("go get failed: %s: %s", buf.String(), err.Error())
	}

	return nil
}

// GoRun runs the runs a command
func GoRun(cmd string) string {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("gorun.Error: %+s", err)
		}
	}()
	var cmdline []string
	com := strings.Split(cmd, " ")

	if len(com) < 0 {
		return ""
	}

	if len(com) == 1 {
		cmdline = append(cmdline, com...)
	} else {
		cmdline = append(cmdline, com[0])
		cmdline = append(cmdline, com[1:]...)
	}

	//setup the executor and use a shard buffer
	cmdo := exec.Command(cmdline[0], cmdline[1:]...)
	buf := bytes.NewBuffer([]byte{})
	cmdo.Stdout = buf
	cmdo.Stderr = buf

	_ = cmdo.Run()

	return buf.String()
}

// GobuildArgs runs the build process and returns true/false and an error, allowing passing in org args
func GobuildArgs(args []string) error {
	if len(args) <= 0 {
		return nil
	}

	defer func() {
		if err := recover(); err != nil {
			log.Printf("gobuild.Error: %+s", err)
		}
	}()

	cmdline := []string{"go", "build"}

	// if runtime.GOOS == "windows" {
	// 	name = fmt.Sprintf("%s.exe", name)
	// }

	// target := filepath.Join(dir, name)
	cmdline = append(cmdline, args...)

	//setup the executor and use a shard buffer
	cmd := exec.Command("go", cmdline[1:]...)
	buf := bytes.NewBuffer([]byte{})

	msg, err := cmd.CombinedOutput()

	if !cmd.ProcessState.Success() {
		return fmt.Errorf("go.build failed: %s: %s -> Msg: %s", buf.String(), err.Error(), msg)
	}

	// fmt.Printf("go.build.Response(%+s): %s", cmdline, msg)

	return nil
}

// Gobuild runs the build process and returns true/false and an error, this works by building in the current root i.e cwd(current working directory)
func Gobuild(dir, name string, args []string) error {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("gobuild.Error: %+s", err)
		}
	}()

	cmdline := []string{"go", "build"}

	if runtime.GOOS == "windows" {
		name = fmt.Sprintf("%s.exe", name)
	}

	target := filepath.Join(dir, name)
	cmdline = append(cmdline, args...)
	cmdline = append(cmdline, "-o", target)

	//setup the executor and use a shard buffer
	cmd := exec.Command("go", cmdline[1:]...)
	buf := bytes.NewBuffer([]byte{})

	msg, err := cmd.CombinedOutput()

	if !cmd.ProcessState.Success() {
		return fmt.Errorf("go.build failed: %s: %s -> Msg: %s", buf.String(), err.Error(), msg)
	}

	// fmt.Printf("go.build.Response(%+s): %s", cmdline, msg)

	return nil
}

// RunCMD runs the a set of commands from a list while skipping any one-length command, panics if it gets an empty lists
func RunCMD(cmds []string, done func()) chan bool {
	if len(cmds) < 0 {
		panic("commands list cant be empty")
	}

	var relunch = make(chan bool)

	go func() {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("cmdRun.Error: %+s", err)
			}
		}()

	cmdloop:
		for {
			select {
			case do, ok := <-relunch:

				if !ok {
					break cmdloop
				}

				if !do {
					continue
				}

				fmt.Printf("--> Running Commands %s\n", cmds)
				for _, cox := range cmds {

					cmd := strings.Split(cox, " ")

					if len(cmd) <= 1 {
						continue
					}

					cmdo := exec.Command(cmd[0], cmd[1:]...)
					cmdo.Stdout = os.Stdout
					cmdo.Stderr = os.Stderr

					if err := cmdo.Start(); err != nil {
						fmt.Printf("---> Error executing command: %s -> %s\n", cmd, err)
					}
				}

				if done != nil {
					done()
				}
			}
		}

	}()
	return relunch
}

// RunGo runs the generated binary file with the arguments expected
func RunGo(gofile string, args []string, done, stopped func()) chan bool {
	var relunch = make(chan bool)

	// if runtime.GOOS == "windows" {
	gofile = filepath.Clean(gofile)
	// }

	go func() {

		// var cmdline = fmt.Sprintf("go run %s", gofile)
		cmdargs := append([]string{"run", gofile}, args...)
		// cmdline = strings.Joinappend([]string{}, "go run", gofile)

		var proc *os.Process

		for dosig := range relunch {
			if proc != nil {
				var err error

				if runtime.GOOS == "windows" {
					err = proc.Kill()
				} else {
					err = proc.Signal(os.Interrupt)
				}

				if err != nil {
					fmt.Printf("---> Error in Sending Kill Signal %s\n", err)
					proc.Kill()
				}
				proc.Wait()
				proc = nil
			}

			if !dosig {
				continue
			}

			fmt.Printf("--> Starting cmd: %s\n", cmdargs)
			cmd := exec.Command("go", cmdargs...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			if err := cmd.Start(); err != nil {
				fmt.Printf("---> Error starting process: %s\n", err)
			}

			proc = cmd.Process
			if done != nil {
				done()
			}
		}

		if stopped != nil {
			stopped()
		}
	}()
	return relunch
}

// RunBin runs the generated binary file with the arguments expected
func RunBin(binfile string, args []string, done, stopped func()) chan bool {
	var relunch = make(chan bool)
	go func() {
		// binfile := fmt.Sprintf("%s/%s", bindir, bin)
		// cmdline := append([]string{bin}, args...)
		var proc *os.Process

		for dosig := range relunch {
			if proc != nil {
				var err error

				if runtime.GOOS == "windows" {
					err = proc.Kill()
				} else {
					err = proc.Signal(os.Interrupt)
				}

				if err != nil {
					fmt.Printf("---> Error in Sending Kill Signal: %s\n", err)
					proc.Kill()
				}
				proc.Wait()
				proc = nil
			}

			if !dosig {
				continue
			}

			fmt.Printf("--> Starting bin: %s\n", binfile)
			cmd := exec.Command(binfile, args...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			if err := cmd.Start(); err != nil {
				fmt.Printf("---> Error starting process: %s -> %s\n", binfile, err)
			}

			proc = cmd.Process
			if done != nil {
				done()
			}
		}

		if stopped != nil {
			stopped()
		}
	}()
	return relunch
}
