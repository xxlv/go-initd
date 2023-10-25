package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "embed"

	"github.com/BurntSushi/toml"
	"github.com/fatih/color"
)

//go:embed initd.tpl
var configTpl string

type RunStat struct {
	sync.Mutex
	isRunning bool
	isStoping bool
	isKilled  bool

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
		Name    string   `toml:"name"`
		Cmd     string   `toml:"cmd"`
		Args    []string `toml:"args"`
		Disable bool     `toml:"disable"`
	} `toml:"services"`
}

func logf(format string, a ...any) {
	log.Default().Printf(format+"\n", a...)
}

func errorf(format string, a ...any) {
	log.Default().Printf(format+"\n", a...)
}

func warnf(format string, a ...any) {
	log.Default().Printf(color.YellowString(fmt.Sprintf(format+"\n", a...)))
}

// doRunAndWatch will loop all command and execute run.
func doRunAndWatch(rs map[string]*RunStat) {
	running := make(map[string]bool)
	// run all
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
				} else if s.Err != nil && !running[name] {
					running[name] = true
					warnf("%s start failed, error is %s", color.HiRedString(name), s.Err.Error())
				}
				s.Unlock()
			}
			if len(running) == len(rs) {
				logf("all program started ")
				break
			}
		}
	}()
	// check
	go func() {
		anyAlivePrintOnceFlag := false
		for {
			// check pid every 1 second
			ticker := time.NewTicker(1 * time.Second)
			select {
			case _ = <-ticker.C:
				anyAlive := false
				for _, s := range rs {
					healthyCheck(s)
					if !s.isKilled {
						anyAlive = true
					}
				}
				if !anyAlive && !anyAlivePrintOnceFlag {
					warnf("*all pids is killed")
					anyAlivePrintOnceFlag = true
				}
				ticker.Reset(1 * time.Second)
			}

		}

	}()
}

// check pid and update stat
func healthyCheck(s *RunStat) (err error) {
	if !s.isKilled && s.Pid > 0 {
		err = testPid(s.Pid)
		if err != nil {
			errorf(color.RedString(fmt.Sprintf("pid %d is killed", s.Pid)))
			s.isKilled = true
		}
	}
	return
}

// test if pid is exists ,if not return error
func testPid(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		errorf("can not find pid %d,error: %s", pid, err.Error())
	}

	if process == nil {
		errorf("pid %s maybe killed", color.CyanString(fmt.Sprint(pid)))
	}
	err = process.Signal(syscall.Signal(0))
	if err != nil {

		return fmt.Errorf("process %d does not exist: %s", pid, err)
	}
	return nil
}

func runServices(c Config) (runningmap map[string]*RunStat, err error) {
	if len(c.Services) <= 0 {
		err = errors.New("service is empty, please check your config!")
	}
	runningmap = make(map[string]*RunStat)
	for _, s := range c.Services {
		cmd := s.Cmd
		name := s.Name
		var runErr error
		if !fileExist(cmd) {
			err = fmt.Errorf("program %s not found", cmd)
		} else if s.Disable {
			runErr = fmt.Errorf("program %s disabled", cmd)
		}

		if runErr != nil {
			runningmap[name] = &RunStat{
				Err:     runErr,
				Command: nil,
				CmdPath: cmd,
				Name:    name,
			}
			continue
		}

		command, err := run(name, cmd, s.Args)
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

func dirname(name string) (d string) {
	if name == "" {
		return ""
	}
	path, _ := filepath.Abs(name)
	index := strings.LastIndex(path, string(os.PathSeparator))
	d = path[:index]
	return
}

func run(name, cmd string, args []string) (command *exec.Cmd, err error) {

	var dname string
	// if cmd not contains `/`, we just run this command (look up for default path)
	if strings.Contains(cmd, string(os.PathSeparator)) {
		cmd, _ = filepath.Abs(cmd)
		dname = dirname(cmd)
	}
	// search dir
	command = exec.Command(cmd, args...)
	if dname != "" {
		command.Dir = dname
	}
	// logf("change dir to %s", dname)
	// TODO: stdout & err
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr

	return
}

func fileExist(fname string) bool {
	if _, err := os.Stat(fname); os.IsNotExist(err) {
		return false
	}
	return true
}

func fileCreate(fname, content string) (err error) {
	if !strings.HasSuffix(fname, ".toml") {
		fname = fmt.Sprintf("%s.toml", fname)
	}
	file, err := os.Create(fname)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer file.Close()
	_, err = file.WriteString(content)
	logf("new file %s created", color.CyanString(fname))

	return
}

// isClosedAll check if all pids are closed.
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
		if rs.isStoping || rs.isKilled {
			continue
		} else {
			rs.isStoping = true
			_ = rs.Stop()
		}
		rs.Unlock()

	}

}

// stop given pid
func stop(pid int) (err error) {
	if pid <= 0 {
		return errors.New("invalid pid `" + fmt.Sprint(pid) + "`")
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		errorf("can not find pid= `%s`, error: %s", color.RedString(fmt.Sprint(pid)), err.Error())
		return
	}
	// send new signal
	err = process.Signal(syscall.Signal(syscall.SIGTERM))
	if err != nil {
		errorf("can not stop pid= `%s`, error: %s", color.RedString(fmt.Sprint(pid)), err.Error())
		return
	}
	logf("success stop pid %s", color.GreenString(fmt.Sprint(pid)))
	return
}

type Initd struct {
	tmpDataPath string
}

// prepare init env
func (i *Initd) prepare() {
	dname, _ := os.MkdirTemp("", "inind")
	i.tmpDataPath = dname
	logf("create new temp dir for current initd , %s", color.CyanString(dname))
}

// clean env
func (i *Initd) destory() {
	err := os.RemoveAll(i.tmpDataPath)
	if err != nil {
		errorf("can not clean path %s, errors: %s", i.tmpDataPath, err.Error())
	}
	logf("clean path %s", color.CyanString(i.tmpDataPath))
}

func (i *Initd) startWith(fname string) {
	i.prepare()
	defer i.destory()

	closedSecond := 1 * time.Second
	content, err := os.ReadFile(fname)
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

func main() {
	prog := &Initd{}
	var config, newp string
	flag.StringVar(&config, "config", "initd.local.toml", "config path")
	flag.StringVar(&newp, "new", "", "create new config file template")
	flag.Parse()

	if newp != "" {
		//  create new one
		fileCreate(newp, configTpl)
		return
	} else if !fileExist(config) {
		flag.Usage = func() {
			fmt.Fprintf(os.Stderr, fmt.Sprintf("Usage of initd \n %s \n", color.BlackString("You can quickly start multiple processes in a simple way.")))
			flag.PrintDefaults()
		}
		flag.Usage()
		return
	} else {
		logf("prepare using config file `%s`", color.BlackString(config))
		prog.startWith(config)
	}

}
