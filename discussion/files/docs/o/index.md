# `o`

`o` is a command for managing objects

##

        25-11-09 scott@air yt % o
        synopsis: o [opts] command [opts]

        o is a tool for working with object notation.

        commands:
            view   view [files]
            eval   eval [-e path=val [ -e path2=val2 ]...] [files]
            diff   diff a b or diff -loop <cmd>
            get    get <objectpath> [files]
            list   list <objectpath> [files]
            match  match [opts] <matchobj> [files]
            patch  patch [opts] <patchobj> [files]
            build  build [dir] [-l] [-p profile ] [ env ]
            dump   dump [files]
            load   load [ir-files]

         options:

         -x         expand <<: merge field while encoding bool
         -color     encode with color                     bool
         -o         output file (defaults to stdout)      string
         -wire      output in compact format              bool
         -t, -tony  do i/o in tony                        bool
         -j, -json  do i/o in json                        bool
         -y, -yaml  do i/o in yaml                        bool
         -I, -ifmt  input format: tony/t, json/j, yaml/y  (format)
         -O, -ofmt  output format: tony/t, json/j, yaml/y (format)

        usage error: no command provided

## Commands

- *[build](/o/build/)*
