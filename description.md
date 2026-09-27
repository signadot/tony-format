# git issue serve: localhost:8080 answers 404 when another program has the port on the other loopback address

`git issue serve` listens on `localhost:8080`, which Go binds as 127.0.0.1 alone. Another program holding port 8080 on IPv6 does not make that fail on macOS, so serve starts and says nothing, and a browser, which resolves `localhost` to ::1 first, reaches the other program.

Seen 2026-09-27: an `apiserver` listening on `*:8080` over IPv6 beside `git issue serve` on 127.0.0.1:8080.

```
http://localhost:8080/     404  from ::1          (apiserver: 404 page not found)
http://127.0.0.1:8080/     200  from 127.0.0.1    (git issue serve)
```

serve printed `http://127.0.0.1:8080/`, which works; the docs and `git issue -h` say `http://localhost:8080/`, which did not. It read as serve having its page somewhere other than the root.

## Fix

- For `localhost`, serve listens on both loopback addresses, 127.0.0.1 and ::1, so the name reaches it however it resolves. A machine with no IPv6 loopback serves the one it has.
- Before it listens on an address it asks whether something already answers there, and refuses if so, naming the address. A bind alone does not find a program that holds the port on a wildcard address: macOS lets the more specific bind succeed, and the two then share the port by address.
- It prints the URL to use, and the docs say to use the one it prints.