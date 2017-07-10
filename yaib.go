package main

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	"github.com/jessevdk/go-flags"
	"github.com/sjoerdsimons/fakemachine"

	"gopkg.in/yaml.v2"
)

func CleanPath(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}

	cwd, _ := os.Getwd()
	return filepath.Join(cwd, path)
}

func CopyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := ioutil.TempFile(filepath.Dir(dst), "")
	if err != nil {
		return err
	}
	_, err = io.Copy(tmp, in)
	if err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err = tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err = os.Chmod(tmp.Name(), mode); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

func CopyTree(sourcetree, desttree string) {
	fmt.Printf("Overlaying %s on %s\n", sourcetree, desttree)
	walker := func(p string, info os.FileInfo, err error) error {

		if err != nil {
			return err
		}

		suffix, _ := filepath.Rel(sourcetree, p)
		target := path.Join(desttree, suffix)
		if info.Mode().IsDir() {
			fmt.Printf("D> %s -> %s\n", p, target)
			os.Mkdir(target, info.Mode())
		} else if info.Mode().IsRegular() {
			fmt.Printf("F> %s\n", p)
			CopyFile(p, target, info.Mode())
		} else {
			log.Panic("Not handled")
		}

		return nil
	}

	filepath.Walk(sourcetree, walker)
}

type QemuHelper struct {
	qemusrc    string
	qemutarget string
}

func NewQemuHelper(context YaibContext) QemuHelper {
	q := QemuHelper{}

	switch context.Architecture {
	case "armhf", "armel", "arm":
		q.qemusrc = "/usr/bin/qemu-arm-static"
	case "arm64":
		q.qemusrc = "/usr/bin/qemu-aarch64-static"
	case "amd64":
		/* Dummy, no qemu */
	default:
		log.Panicf("Don't know qemu for Architecture %s", context.Architecture)
	}

	if q.qemusrc != "" {
		q.qemutarget = path.Join(context.rootdir, q.qemusrc)
	}

	return q
}

func (q QemuHelper) Setup() error {
	if q.qemusrc == "" {
		return nil
	}
	return CopyFile(q.qemusrc, q.qemutarget, 755)
}

func (q QemuHelper) Cleanup() {
	if q.qemusrc != "" {
		os.Remove(q.qemutarget)
	}
}

func RunCommand(label, command string, arg ...string) error {
	cmd := exec.Command(command, arg...)

	output, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	cmd.Start()

	fmt.Printf("Running %s: %s %v\n", label, command, arg)

	scanner := bufio.NewScanner(output)
	for scanner.Scan() {
		fmt.Printf("%s | %s\n", label, scanner.Text())
	}

	reader := bufio.NewReader(stderr)

	for {
		line, _, err := reader.ReadLine()

		if err == io.EOF {
			break
		} else if err != nil {
			fmt.Printf("FAILED: %v\n", err)
			break
		}
		fmt.Printf("%s E | %s\n", label, line)
	}

	err := cmd.Wait()

	return err
}

func RunCommandInChroot(context YaibContext, label, command string, arg ...string) error {
	options := []string{"-D", context.rootdir, command}
	options = append(options, arg...)

	q := NewQemuHelper(context)
	q.Setup()
	defer q.Cleanup()

	return RunCommand(label, "systemd-nspawn", options...)
}

type YaibContext struct {
	rootdir      string
	artifactdir  string
	image        string
	imageMntDir  string
	Architecture string
}

type Action interface {
	Verify(context YaibContext)
	PreMachine(context *YaibContext, m *fakemachine.Machine, args *[]string)
	Run(context *YaibContext)
	Cleanup(context YaibContext)
	PostMachine(context YaibContext)
}

type BaseAction struct{}

func (b *BaseAction) Verify(context YaibContext) {}
func (b *BaseAction) PreMachine(context *YaibContext,
	m *fakemachine.Machine,
	args *[]string) {
}
func (b *BaseAction) Run(context *YaibContext)        {}
func (b *BaseAction) Cleanup(context YaibContext)     {}
func (b *BaseAction) PostMachine(context YaibContext) {}

/* the YamlAction just embed the Action interface and implements the
 * UnmarshalYAML function so it can select the concrete implementer of a
 * specific action at unmarshaling time */
type YamlAction struct {
	Action
}

func (y *YamlAction) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var aux struct {
		Action string
	}
	err := unmarshal(&aux)
	if err != nil {
		return err
	}

	switch aux.Action {
	case "debootstrap":
		y.Action = &DebootstrapAction{}
	case "pack":
		y.Action = &PackAction{}
	case "unpack":
		y.Action = &UnpackAction{}
	case "run":
		y.Action = &RunAction{}
	case "overlay":
		y.Action = &OverlayAction{}
	case "setup-image":
		y.Action = &SetupImage{}
	case "deploy-image":
		y.Action = &DeployImageAction{}
	default:
		panic(fmt.Sprintf("Unknown action: %v", aux.Action))
	}

	unmarshal(y.Action)

	return nil
}

type Recipe struct {
	Architecture string
	Actions      []YamlAction
}

func main() {
	var context YaibContext
	var options struct {
		ArtifactDir   string `long:"artifactdir"`
		InternalImage string `long:"internal-image" hidden:"true"`
	}

	parser := flags.NewParser(&options, flags.Default)
	args, err := parser.Parse()

	if err != nil {
		flagsErr, ok := err.(*flags.Error)
		if ok && flagsErr.Type == flags.ErrHelp {
			os.Exit(0)
		} else {
			fmt.Printf("%v\n", flagsErr)
			os.Exit(1)
		}
	}

	context.rootdir = "/scratch/rootdir"
	context.artifactdir = options.ArtifactDir
	context.image = options.InternalImage

	file := args[0]

	data, err := ioutil.ReadFile(file)
	if err != nil {
		panic(err)
	}

	r := Recipe{}

	err = yaml.Unmarshal(data, &r)
	if err != nil {
		panic(err)
	}

	context.Architecture = r.Architecture

	for _, a := range r.Actions {
		a.Verify(context)
	}

	if !fakemachine.InMachine() {
		m := fakemachine.NewMachine()
		var args []string

		if context.artifactdir == "" {
			context.artifactdir, _ = os.Getwd()
		}

		context.artifactdir = CleanPath(context.artifactdir)

		m.AddVolume(context.artifactdir)
		args = append(args, "--artifactdir", context.artifactdir)

		file = CleanPath(file)
		m.AddVolume(path.Dir(file))
		args = append(args, file)

		for _, a := range r.Actions {
			a.PreMachine(&context, m, &args)
		}

		ret := m.RunInMachineWithArgs(args)

		for _, a := range r.Actions {
			a.PostMachine(context)
		}

		os.Exit(ret)
	}

	for _, a := range r.Actions {
		a.Run(&context)
	}

	for _, a := range r.Actions {
		a.Cleanup(context)
	}
}
