# codegen: comment= and lineComment= capture on decode and are dropped on encode

The schemagen annotations `comment=<Field>` and `lineComment=<Field>` (codegen spells the
second `LineComment=`) let a struct carry its own head and line comments as Go fields. They
work in one direction only: what is captured on decode is silently dropped on encode, so a
document read into a struct and written back loses the comments it arrived with.

REFLECTION (gomap/mapper_to.go:113-127) says so in its own words:

    if structSchema.CommentFieldName != "" {
        commentField := val.FieldByName(structSchema.CommentFieldName)
        if commentField.IsValid() && commentField.CanSet() {
            // Comments are stored on the IR node itself
            // We'll populate this when we have access to the source IR node
            // For now, this is a placeholder for when marshaling from IR -> IR
        }
    }

CODEGEN has the same shape: gomap/codegen/generator.go:1289-1303 emits the capture into the
generated FromTonyIR (unwrap CommentType, `s.<Field> = node.Lines`, `s.<Field> = node.Comment.Lines`),
and the ToTonyIR generator emits nothing for either field.

Observed with go-tony v0.0.141, reflection path:

    type Rule struct {
        marker `tony:"schemagen=rule,notag,comment=Head,lineComment=Line"`
        Head   []string `tony:"-"`
        Name   string   `tony:"field=name"`
    }

    in:  "# why this rule exists\nname: review\n"
    decoded: Head=["# why this rule exists"]      <- captured
    re-encoded: "name: review\n"                  <- dropped

WHY IT MATTERS NOW. v0.0.141 made the store keep comments — NextState applies patches with them
and SameState counts them — so a comment is durable data for the first time. A struct with a
generated codec is exactly what cannot carry one through: verse stores a charter by decoding it
to a Go struct and re-encoding (`t.ToTonyIR()`), so an installed rule loses every comment its
author wrote, while a payload verse never decodes keeps them. The annotation is the mechanism
that would fix it and is the half that is not built.

SECOND, SMALLER: the option is spelled two ways. gomap/tags.go:369 reads `lineComment`;
gomap/codegen/parser.go:231,535 read `LineComment`. A struct annotated for one path is silently
ignored by the other — no error, just an empty field.