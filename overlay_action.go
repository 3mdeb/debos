package main

import (
	"path"
)

type OverlayAction struct {
	*BaseAction
	Source string
}

func (overlay *OverlayAction) Run(context *YaibContext) error {
	sourcedir := path.Join(context.recipeDir, overlay.Source)
	return CopyTree(sourcedir, context.rootdir)
}
