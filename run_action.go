package main

type RunAction struct {
	Chroot bool
	Script string
}

func (run *RunAction) Run(context YaibContext) {
	err := RunCommandInChroot(context, run.Script, "sh", "-c", run.Script)
	if err != nil {
		panic(err)
	}
}
