package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/fatih/color"
)

type RunStat struct {
	sync.Mutex
	isRunning bool
	isStoping bool

	Name    string
	CmdPath string
	Command *exec.Cmd
	Pid     int
	Err     error

	runAt time.Time
}

func (rs *RunStat) Update() {
	if rs.Command != nil && rs.Pid <= 0 {
		if rs.Command.Process != nil {
			rs.Pid = rs.Command.Process.Pid
		}
	}
}
func (rs *RunStat) Stop() error {
	return stop(rs.Pid)
}

type Config struct {
	Services []struct {
		Name string `toml:"name"`
		Cmd  string `toml:"cmd"`
	} `toml:"services"`
}

func logf(format string, a ...any) {
	log.Default().Printf(format+"\n", a...)
}

func errorf(format string, a ...any) {
	log.Fatalf(format, a...)
}

func doRunAndWatch(rs map[string]*RunStat) {
	running := make(map[string]bool)
	go func() {
		for {
			for name, s := range rs {
				s.Lock()
				if !s.isRunning && !s.isStoping {
					if s.Command != nil {
						go s.Command.Run()
						s.isRunning = true
						s.runAt = time.Now()
					}
				}
				if s.Pid <= 0 {
					s.Update()
				}
				if s.Pid > 0 && !running[name] {
					logf("new pid %s [pid=%s] create for prog name `%s` ", color.CyanString(s.CmdPath), color.GreenString(fmt.Sprint(s.Pid)), color.MagentaString(name))

					running[name] = true
				}
				s.Unlock()
			}
			if len(running) == len(rs) {
				logf("all program started ")
				break
			}

		}
	}()
}

func runServices(c Config) (runningmap map[string]*RunStat, err error) {
	if len(c.Services) <= 0 {
		err = errors.New("service is empty, please check your config!")
	}
	runningmap = make(map[string]*RunStat)
	for _, s := range c.Services {
		cmd := s.Cmd
		name := s.Name
		command, err := run(name, cmd)

		runningmap[name] = &RunStat{
			Err:     err,
			Command: command,
			CmdPath: cmd,
			Name:    name,
		}

	}
	doRunAndWatch(runningmap)

	return
}

func run(name, cmd string) (command *exec.Cmd, err error) {
	command = exec.Command(cmd)
	// TODO: stdout & err
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return
}

func main() {
	closedSecond := 5 * time.Second

	content, err := os.ReadFile("initd.toml")
	if err != nil {
		log.Fatal(err)
		return
	}
	var config Config
	if _, err := toml.Decode(string(content), &config); err != nil {
		log.Fatal(err)
		return
	}
	c := make(chan os.Signal)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	sermap, _ := runServices(config)
	<-c
	shutdown(sermap)

	ticker := time.NewTicker(closedSecond)
	select {
	case _ = <-ticker.C:
		if isClosedAll(sermap) {
			logf("all program closed")
			return
		}
	}
}

func isClosedAll(runningmap map[string]*RunStat) bool {

	for _, rs := range runningmap {
		if rs.isRunning {
			return true
		}
	}
	return false
}

// shutdown running map pid .
// we notify pid using SIGTERM
func shutdown(runningmap map[string]*RunStat) {
	if len(runningmap) <= 0 {
		return
	}
	for _, rs := range runningmap {
		rs.Lock()
		if rs.isStoping {
			continue
		} else {
			rs.isStoping = true
			_ = rs.Stop()
		}
		rs.Unlock()

	}

}

// stop given Pid
func stop(pid int) (err error) {
	if pid <= 0 {
		return errors.New("invalid pid `" + fmt.Sprint(pid) + "`")
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		errorf("can not find pid= `%s`, error: %s", color.RedString(fmt.Sprint(pid)), err.Error())
		return
	}
	err = process.Signal(syscall.SIGTERM)
	if err != nil {
		errorf("can not stop pid= `%s`, error: %s", color.RedString(fmt.Sprint(pid)), err.Error())
		return
	}
	logf("success stop pid %s", color.GreenString(fmt.Sprint(pid)))
	return
}
