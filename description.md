# eval: an expression error inside a container under !eval is dropped, and a line comment is expanded twice

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); reproduced at 546bd53.

    printf 'a: !eval {x: ".[1 +]"}\n' | o eval    ->  exit 0, the expression unexpanded

The top-level form exits 2. eval/expand_env.go:80 and :84 discard ExpandEnv's error for a
container's children. It reaches `o eval`, and `o build` through tony.Tool.

Also: ExpandEnv expands a line comment twice (the blocks at :58 and :67): with x="$[y]"
and y=2, `# c=$[x]` becomes `# c=2`. Library only -- `o eval` drops comments.