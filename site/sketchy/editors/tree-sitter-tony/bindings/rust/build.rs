fn main() {
    cc::Build::new()
        .file("parser.c")
        .include(".")
        .compile("tree-sitter-tony");
}
