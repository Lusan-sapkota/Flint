#!/usr/bin/env python3
"""
build_docs.py - Generates static, zero-dependency HTML documentation for Flint.
Preserves 100% of markdown content and context while delivering a fast,
offline-capable, responsive experience matching Flint's dark UI aesthetic.
"""

import os
import re
import shutil
import markdown
from bs4 import BeautifulSoup

NAV_STRUCTURE = [
    {
        "group": "Get started",
        "items": [
            {"title": "Overview", "file": "README.md", "url": "index.html", "desc": "Offline chat UI for local Ollama models"},
            {"title": "Install and run", "file": "deployment.md", "url": "deployment.html", "desc": "Docker, source, environment variables, health checks"},
        ]
    },
    {
        "group": "Using Flint",
        "items": [
            {"title": "Features", "file": "features.md", "url": "features.html", "desc": "Chats, tools, agents, memory, web search, and settings"},
            {"title": "Security", "file": "security.md", "url": "security.html", "desc": "Accounts, rate limits, command shield, and preconditions"},
        ]
    },
    {
        "group": "How it works",
        "items": [
            {"title": "Architecture", "file": "architecture.md", "url": "architecture.html", "desc": "Architecture, data model, chat turns, stream protocol"},
            {"title": "Context management", "file": "context-management.md", "url": "context-management.html", "desc": "Budgeting, fitting, layered summaries, and verbatim retention"},
        ]
    },
    {
        "group": "Evidence",
        "items": [
            {"title": "Experiments", "file": "experiments.md", "url": "experiments.html", "desc": "Measurements E1–E25 behind every design decision"},
            {"title": "Benchmark", "file": "benchmark.md", "url": "benchmark.html", "desc": "Scored benchmark tasks and ablation results"},
            {"title": "Testing", "file": "testing.md", "url": "testing.html", "desc": "Unit tests, live verification, long chats, and re-ranking"},
        ]
    },
    {
        "group": "Project",
        "items": [
            {"title": "Changelog", "file": "changelog.md", "url": "changelog.html", "desc": "Release history and version comparisons"},
            {"title": "Contributing", "url": "https://github.com/Lusan-sapkota/Flint/blob/main/CONTRIBUTING.md", "desc": "How to contribute, guidelines, and release steps", "external": True},
        ]
    }
]

# Linear reading order
LINEAR_PAGES = [
    item for group in NAV_STRUCTURE for item in group["items"] if not item.get("external")
]

def slugify(text, sep="-"):
    """GitHub / Kramdown compatible slugify for heading anchors."""
    # Strip HTML tags
    t = re.sub(r"<[^>]+>", "", text)
    # Lowercase
    t = t.lower()
    # Strip punctuation except hyphens, spaces, alphanumeric
    t = re.sub(r"[^\w\s-]", "", t)
    # Whitespace to separator
    t = re.sub(r"[\s_]+", sep, t).strip(sep)
    return t

class GitHubAnchorExtension(markdown.Extension):
    """Ensures heading IDs match GitHub / kramdown anchors."""
    def extendMarkdown(self, md):
        md.registerExtension(self)

def convert_md_to_html(md_text, current_url):
    """Converts markdown text to HTML with accurate links and anchors."""
    # Rewrite relative .md links before rendering
    # e.g., (features.md#agents-agent) -> (features.html#agents-agent)
    # e.g., (README.md) -> (index.html)
    def link_replacer(match):
        pre = match.group(1) # [text]
        target = match.group(2) # link
        
        # External links untouched
        if target.startswith("http://") or target.startswith("https://") or target.startswith("mailto:"):
            return f"[{pre}]({target})"
        
        # Split anchor if present
        parts = target.split("#", 1)
        path = parts[0]
        anchor = f"#{parts[1]}" if len(parts) > 1 else ""
        
        if path == "README.md":
            path = "index.html"
        elif path.endswith(".md"):
            path = path[:-3] + ".html"
        elif path == "CONTRIBUTING.md":
            return f"[{pre}](https://github.com/Lusan-sapkota/Flint/blob/main/CONTRIBUTING.md{anchor})"
        elif path == "LICENSE" or path == "./LICENSE":
            return f"[{pre}](https://github.com/Lusan-sapkota/Flint/blob/main/LICENSE)"
            
        return f"[{pre}]({path}{anchor})"

    processed_md = re.sub(r"\[([^\]]+)\]\(([^)]+)\)", link_replacer, md_text)
    
    md = markdown.Markdown(
        extensions=[
            "extra",
            "toc",
            "sane_lists",
            "codehilite",
        ],
        extension_configs={
            "toc": {
                "slugify": slugify,
                "permalink": False,
            },
            "codehilite": {
                "css_class": "highlight",
                "guess_lang": False,
            }
        }
    )
    html = md.convert(processed_md)
    
    # Process HTML with BeautifulSoup to enhance tables, headings, and code blocks
    soup = BeautifulSoup(html, "html.parser")
    
    # Wrap tables for responsive scrolling
    for table in soup.find_all("table"):
        wrapper = soup.new_tag("div", **{"class": "table-wrapper"})
        table.wrap(wrapper)
        
    # Add anchor links to headings h2, h3, h4
    for h in soup.find_all(["h2", "h3", "h4"]):
        h_id = h.get("id")
        if not h_id:
            h_id = slugify(h.get_text())
            h["id"] = h_id
        anchor = soup.new_tag("a", href=f"#{h_id}", **{"class": "header-anchor", "aria-label": f"Link to {h.get_text()}"})
        anchor.string = "#"
        h.append(anchor)
        
    # Enhance code blocks with copy button wrapper
    for pre in soup.find_all("pre"):
        container = soup.new_tag("div", **{"class": "code-block"})
        pre.wrap(container)
        btn = soup.new_tag("button", **{
            "class": "copy-code-btn",
            "type": "button",
            "aria-label": "Copy code to clipboard",
            "title": "Copy code"
        })
        btn.string = "Copy"
        container.insert(0, btn)
        
    return str(soup)

def build_nav_html(current_url):
    nav_html = []
    for group in NAV_STRUCTURE:
        nav_html.append(f'<div class="nav-group">')
        nav_html.append(f'  <div class="nav-group-title">{group["group"]}</div>')
        nav_html.append(f'  <ul class="nav-group-items">')
        for item in group["items"]:
            is_active = (item.get("url") == current_url)
            active_cls = ' class="nav-item active" aria-current="page"' if is_active else ' class="nav-item"'
            if item.get("external"):
                nav_html.append(f'    <li><a href="{item["url"]}" target="_blank" rel="noopener noreferrer"{active_cls}>{item["title"]} <span class="external-icon" aria-hidden="true">↗</span></a></li>')
            else:
                nav_html.append(f'    <li><a href="{item["url"]}"{active_cls}>{item["title"]}</a></li>')
        nav_html.append(f'  </ul>')
        nav_html.append(f'</div>')
    return "\n".join(nav_html)

def build_page_template(page_item, content_html):
    current_url = page_item["url"]
    title = f"{page_item['title']} · Flint" if page_item["url"] != "index.html" else "Flint — Offline Chat UI for Local Ollama Models"
    description = page_item.get("desc", "Flint is a small, fully offline chat UI for local Ollama models.")
    
    # Calculate Prev / Next
    current_idx = -1
    for idx, p in enumerate(LINEAR_PAGES):
        if p["url"] == current_url:
            current_idx = idx
            break
            
    prev_item = LINEAR_PAGES[current_idx - 1] if current_idx > 0 else None
    next_item = LINEAR_PAGES[current_idx + 1] if current_idx >= 0 and current_idx < len(LINEAR_PAGES) - 1 else None
    
    prev_next_html = []
    if prev_item or next_item:
        prev_next_html.append('<nav class="page-nav" aria-label="Page navigation">')
        if prev_item:
            prev_next_html.append(f'''
            <a href="{prev_item['url']}" class="page-nav-link prev">
              <span class="page-nav-sub">← Previous</span>
              <span class="page-nav-title">{prev_item['title']}</span>
            </a>
            ''')
        else:
            prev_next_html.append('<div class="page-nav-placeholder"></div>')
            
        if next_item:
            prev_next_html.append(f'''
            <a href="{next_item['url']}" class="page-nav-link next">
              <span class="page-nav-sub">Next →</span>
              <span class="page-nav-title">{next_item['title']}</span>
            </a>
            ''')
        prev_next_html.append('</nav>')
    
    nav_html = build_nav_html(current_url)
    
    return f'''<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{title}</title>
  <meta name="description" content="{description}">
  <link rel="icon" type="image/x-icon" href="favicon.ico">
  <link rel="stylesheet" href="assets/css/docs.css">
</head>
<body>
  <div class="docs-layout">
    <!-- Mobile Header -->
    <header class="mobile-header" aria-label="Mobile header">
      <button class="mobile-menu-toggle" id="menuToggle" aria-label="Open navigation menu" aria-expanded="false">
        <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <line x1="3" y1="12" x2="21" y2="12"></line>
          <line x1="3" y1="6" x2="21" y2="6"></line>
          <line x1="3" y1="18" x2="21" y2="18"></line>
        </svg>
      </button>
      <a href="index.html" class="mobile-brand">
        <img src="images/logo.png" alt="Flint logo" width="28" height="28" class="mobile-logo">
        <span class="mobile-brand-name">Flint</span>
        <span class="version-badge">v0.2.0</span>
      </a>
      <a href="https://github.com/Lusan-sapkota/Flint" class="mobile-github" aria-label="GitHub repository" target="_blank" rel="noopener noreferrer">
        <svg width="20" height="20" viewBox="0 0 24 24" fill="currentColor">
          <path fill-rule="evenodd" clip-rule="evenodd" d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.53 1.032 1.53 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0112 6.844c.85.004 1.705.115 2.504.337 1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.019 10.019 0 0022 12.017C22 6.484 17.522 2 12 2z"/>
        </svg>
      </a>
    </header>

    <div class="sidebar-backdrop" id="sidebarBackdrop"></div>

    <!-- Sidebar Navigation -->
    <aside class="docs-sidebar" id="sidebar" aria-label="Documentation navigation">
      <div class="sidebar-header">
        <a href="index.html" class="sidebar-brand">
          <img src="images/logo.png" alt="Flint logo" width="36" height="36" class="brand-logo">
          <div class="brand-text">
            <div class="brand-title-wrap">
              <span class="brand-title">Flint</span>
              <span class="version-badge">v0.2.0</span>
            </div>
            <span class="brand-tagline">Offline Ollama UI</span>
          </div>
        </a>
      </div>

      <div class="sidebar-links">
        <a href="https://github.com/Lusan-sapkota/Flint" class="sidebar-link-btn" target="_blank" rel="noopener noreferrer">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor">
            <path fill-rule="evenodd" clip-rule="evenodd" d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.53 1.032 1.53 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0112 6.844c.85.004 1.705.115 2.504.337 1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.019 10.019 0 0022 12.017C22 6.484 17.522 2 12 2z"/>
          </svg>
          <span>GitHub</span>
          <span class="external-icon">↗</span>
        </a>
        <a href="https://github.com/Lusan-sapkota/Flint/releases" class="sidebar-link-btn" target="_blank" rel="noopener noreferrer">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
            <polyline points="7 10 12 15 17 10"></polyline>
            <line x1="12" y1="15" x2="12" y2="3"></line>
          </svg>
          <span>Releases</span>
          <span class="external-icon">↗</span>
        </a>
      </div>

      <nav class="sidebar-nav" aria-label="Documentation pages">
        {nav_html}
      </nav>

      <div class="sidebar-footer">
        <span class="sidebar-footer-text">Flint Documentation</span>
        <span class="sidebar-footer-sub">Zero build step · 100% offline</span>
      </div>
    </aside>

    <!-- Main Content Container -->
    <main class="docs-main" id="mainContent">
      <article class="docs-content">
        {content_html}
        {chr(10).join(prev_next_html)}
      </article>

      <footer class="docs-page-footer">
        <div class="docs-footer-inner">
          <p class="docs-footer-copy">Flint is licensed under <a href="https://github.com/Lusan-sapkota/Flint/blob/main/LICENSE" target="_blank" rel="noopener noreferrer">AGPL-3.0</a>.</p>
          <a href="#mainContent" class="back-to-top" aria-label="Back to top of page">
            <span>Back to top</span>
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <polyline points="18 15 12 9 6 15"></polyline>
            </svg>
          </a>
        </div>
      </footer>
    </main>
  </div>

  <script src="assets/js/docs.js"></script>
</body>
</html>
'''

def main():
    src_dir = "docs_backup"
    out_dir = "docs_html"
    
    if not os.path.exists(src_dir):
        print(f"Error: {src_dir} not found")
        return
        
    os.makedirs(out_dir, exist_ok=True)
    os.makedirs(os.path.join(out_dir, "assets", "css"), exist_ok=True)
    os.makedirs(os.path.join(out_dir, "assets", "js"), exist_ok=True)
    
    # Copy images, tools, CNAME, favicon
    if os.path.exists(os.path.join(src_dir, "images")):
        shutil.copytree(os.path.join(src_dir, "images"), os.path.join(out_dir, "images"), dirs_exist_ok=True)
    if os.path.exists(os.path.join(src_dir, "tools")):
        shutil.copytree(os.path.join(src_dir, "tools"), os.path.join(out_dir, "tools"), dirs_exist_ok=True)
    if os.path.exists(os.path.join(src_dir, "CNAME")):
        shutil.copy2(os.path.join(src_dir, "CNAME"), os.path.join(out_dir, "CNAME"))
    if os.path.exists(os.path.join(src_dir, "favicon.ico")):
        shutil.copy2(os.path.join(src_dir, "favicon.ico"), os.path.join(out_dir, "favicon.ico"))
        
    # Write .nojekyll so GitHub Pages does not run Jekyll
    with open(os.path.join(out_dir, ".nojekyll"), "w") as f:
        f.write("")
        
    # Render all pages
    rendered_count = 0
    for page in LINEAR_PAGES:
        src_path = os.path.join(src_dir, page["file"])
        out_path = os.path.join(out_dir, page["url"])
        
        with open(src_path, "r", encoding="utf-8") as f:
            md_text = f.read()
            
        content_html = convert_md_to_html(md_text, page["url"])
        full_html = build_page_template(page, content_html)
        
        with open(out_path, "w", encoding="utf-8") as f:
            f.write(full_html)
            
        print(f"Rendered {page['file']} -> {page['url']} ({len(full_html)} bytes)")
        rendered_count += 1
        
    print(f"Successfully generated {rendered_count} pages in {out_dir}")

if __name__ == "__main__":
    main()
