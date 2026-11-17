"""Render an article's title on the site from its front matter.

An article under docs/articles/ is written for publication elsewhere, and its
destination renders the title from the front matter. Its body therefore has no
H1, and a heading added to the body would show twice there. This hook supplies
the heading when the site is built, and under it, for an article with a
canonical_url, where and when the article was first published.
"""

from urllib.parse import urlparse


def on_page_markdown(markdown, page, config, files):
    if not page.file.src_uri.startswith("articles/"):
        return markdown
    title = page.meta.get("title")
    if not title or markdown.lstrip().startswith("# "):
        return markdown
    head = f"# {title}\n\n"
    url = page.meta.get("canonical_url")
    date = page.meta.get("date")
    if url and date:
        host = urlparse(url).netloc
        head += f"*First published {date} at [{host}]({url}).*\n\n"
    return head + markdown
