package encode

import (
	"strings"

	"github.com/signadot/tony-format/go-tony/ir"

	"github.com/fatih/color"
)

type Colorable struct {
	Type ir.Type
	Attr ColorAttr
}

type ColorAttr int

const (
	CommentColor ColorAttr = iota
	TagColor
	FieldColor
	ValueColor
	SepColor
	LiteralSingleColor
	LiteralMultiColor
	MergeColor
	MergeRawColor
)

type Colors struct {
	Default func(string, ...any) string
	Map     map[Colorable]func(string, ...any) string
}

func NewColors() *Colors {
	colors := &Colors{
		Default: colorDefault,
		Map:     map[Colorable]func(string, ...any) string{},
	}
	for _, t := range ir.Types() {
		able := Colorable{
			Type: t,
			Attr: TagColor,
		}
		colors.Map[able] = color.RGB(74, 92, 138).SprintfFunc()
		able.Attr = CommentColor
		colors.Map[able] = color.BlueString
		able.Attr = SepColor
		colors.Map[able] = color.RGB(255, 0, 196).SprintfFunc()
	}
	colors.Map[Colorable{Type: ir.CommentType, Attr: ValueColor}] = color.BlueString
	able := Colorable{Attr: ValueColor}

	able.Type = ir.NumberType
	colors.Map[able] = color.RGB(128, 216, 236).SprintfFunc()
	able.Attr = FieldColor
	colors.Map[able] = color.RGB(196, 96, 16).SprintfFunc()
	able.Attr = ValueColor

	able.Type = ir.NullType
	colors.Map[able] = color.RGB(168, 0, 196).SprintfFunc()

	able.Type = ir.BoolType
	colors.Map[able] = color.CyanString

	able.Type = ir.ObjectType
	able.Attr = FieldColor
	colors.Map[able] = color.RGB(128, 168, 196).SprintfFunc()
	able.Attr = MergeColor
	colors.Map[able] = color.RGB(196, 168, 128).SprintfFunc()
	able.Attr = MergeRawColor
	colors.Map[able] = color.RGB(96, 96, 96).SprintfFunc() // Darker grey color for merge raw content
	able.Attr = SepColor
	colors.Map[able] = color.RGB(196, 128, 128).SprintfFunc()

	able.Type = ir.StringType
	able.Attr = ValueColor
	colors.Map[able] = color.RGB(8, 196, 16).SprintfFunc()
	able.Attr = LiteralMultiColor
	colors.Map[able] = color.RGB(198, 198, 46).SprintfFunc()
	able.Attr = LiteralSingleColor
	colors.Map[able] = color.RGB(88, 158, 86).SprintfFunc()
	for k, f := range colors.Map {
		colors.Map[k] = func(v string, _ ...any) string {
			return f(strings.Replace(v, "%", "%%", -1))
		}
	}
	return colors
}

func colorDefault(v string, _ ...any) string { return v }

func (c *Colors) Color(t ir.Type, a ColorAttr, s string) string {
	res := c.Get(t, a)(s)
	return res
}

func (c *Colors) Get(t ir.Type, a ColorAttr) func(string, ...any) string {
	f := c.Map[Colorable{Type: t, Attr: a}]
	if f == nil {
		return c.Default
	}
	return f
}
