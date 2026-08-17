# schema: array(t) never checks its elements

`accept: .[array(int)]` accepts a list of anything.

    $ printf 'accept: .[array(int)]\n' > s.tony
    $ printf -- '- 1\n- hello\n- {a: 1}\n' | o schema check s.tony
    stdin: ok

Same with a defined element type: .[array(person)] against [3] is ok. Only the
outer !irtype [] is checked, so an array type is an array type and the element
type is decoration.

base.tony defines it as

    array(t): !and
    - .[array]
    - !all.t null

so the element check is the !all.t half, and that half is not doing anything.
Worth checking whether the parameter t is bound at all when the definition is
instantiated, and whether !all composed onto a reference tag resolves.

Found while wiring cmd/o exit codes: a schema check that cannot fail is worse
than no schema check, because it is reported as a pass.

Related, and separate: .[int] matches the IR ENCODING of an integer
({int: !not null}) rather than an integer, so 'age: .[int]' rejects 'age: 3' in
a plain document. Either that is the documented meaning and .[number] is what a
plain schema should say, or int is broken for the ordinary case. It is why the
cmd/o wiring test's schema says number.