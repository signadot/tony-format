# mergeop: a patch keeping comments drops the line comment on an object-valued key -- a stored section heading loses its note while every scalar keeps one

A patch applied with mergeop.Comments(true) keeps a head comment and the line comment on
every scalar, and drops the line comment on a key whose value is an OBJECT. Measured on
main (26df661) and on v0.0.207 alike:

    # about this pr
    stage: open # still open
    labels: # what it is tagged with
      urgent: true # by whom

patched onto null with Comments(true) and encoded with EncodeComments(true) answers

    # about this pr
    labels:
      urgent: true # by whom
    stage: open # still open

"# what it is tagged with" is gone. The parse holds it (encoding the parsed node straight
back shows it), so the loss is in the patch walk. No tag is involved: the same document
under !raw, !insert.raw or !insert loses the same comment and no other.

REPRO (go test, any module with go-tony required):

    func TestPatchDropsLineCommentOnObjectKey(t *testing.T) {
        const doc = "# about this pr\nstage: open # still open\nlabels: # what it is tagged with\n  urgent: true # by whom\n"
        patch, err := parse.Parse([]byte(doc), parse.ParseComments(true))
        if err != nil {
            t.Fatal(err)
        }
        out, err := tony.Patch(ir.Null(), patch, mergeop.Comments(true))
        if err != nil {
            t.Fatal(err)
        }
        var b strings.Builder
        if err := encode.Encode(out, &b, encode.EncodeComments(true)); err != nil {
            t.Fatal(err)
        }
        for _, want := range []string{"# about this pr", "# still open", "# what it is tagged with", "# by whom"} {
            if !strings.Contains(b.String(), want) {
                t.Errorf("the patch lost %q", want)
            }
        }
    }

WHERE. patch.go's Comments handling (keepComments, around line 61) is about the CommentType
WRAPPER: both sides are unwrapped, the value is patched, and rewrapComment puts the wrapper
back. That is the head comment. A line comment is not a wrapper; the IR holds it on the
node itself (Node.Comment -- mergeop/comment.go's lineCommentLines says as much). A scalar
survives because the structural walk answers with the patch's own scalar node, Comment and
all. An object does not, because objMergeFast builds a fresh object node for the result and
nothing copies the patch object's Comment onto it. That reading is from the code, not from
a fix that was tried.

CONSEQUENCE. A store that keeps comments through a write keeps them arbitrarily: a note on
a scalar field survives and a note on a section heading does not, and the writer cannot
tell which by looking at what they wrote. Context: found while moving verse's write escape
from !raw to !insert.raw (verse 75395cec, against this repo's mgg9nvt6h12krn6dksn0). On
v0.0.207 !raw returned its subtree verbatim and verse's charter round-trip never entered the
patch walk; on main !insert.raw applies its child as a patch against absence, so the
round-trip now reads a rule back without `condition: # cause and subject` and
`action: # the write`, while `name: r # the short name` still comes through.