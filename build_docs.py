#!/usr/bin/env python3
"""
build_docs.py - Generates static, modern, zero-dependency HTML documentation for Flint.
Features a 3-column layout inspired by modern documentation sites (like React docs):
  - Sticky Top Navbar with Brand, Search (Ctrl+K), and Quick Links
  - Left Sidebar with grouped hierarchical navigation
  - Center Content column with breadcrumbs, clean reading width, code copy
  - Right Sidebar with "ON THIS PAGE" Table of Contents and ScrollSpy
  - Instant offline Search modal indexing all pages and sections
"""

import os
import re
import json
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

LINEAR_PAGES = [
    item for group in NAV_STRUCTURE for item in group["items"] if not item.get("external")
]

def slugify(text, sep="-"):
    t = re.sub(r"<[^>]+>", "", text)
    t = t.lower()
    t = re.sub(r"[^\w\s-]", "", t)
    t = re.sub(r"[\s_]+", sep, t).strip(sep)
    return t

def convert_md_to_html(md_text):
    def link_replacer(match):
        pre = match.group(1)
        target = match.group(2)
        
        if target.startswith("http://") or target.startswith("https://") or target.startswith("mailto:"):
            return f"[{pre}]({target})"
        
        parts = target.split("#", 1)
        path = parts[0]
        anchor = f"#{parts[1]}" if len(parts) > 1 else ""
        
        if path == "README.md":
            path = "index.html"
        elif path.endswith(".md"):
            path = path[:-3] + ".html"
        elif path == "CONTRIBUTING.md":
            return f"[{pre}](https://github.com/Lusan-sapkota/Flint/blob/main/CONTRIBUTING.md{anchor})"
        elif path in ("LICENSE", "./LICENSE"):
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
    soup = BeautifulSoup(html, "html.parser")
    
    # Wrap tables
    for table in soup.find_all("table"):
        wrapper = soup.new_tag("div", **{"class": "table-wrapper"})
        table.wrap(wrapper)
        
    # Ensure h1 has id="overview"
    h1 = soup.find("h1")
    if h1:
        h1["id"] = "overview"

    # Extract TOC items and add anchor links
    toc_items = []
    for h in soup.find_all(["h2", "h3"]):
        h_id = h.get("id")
        h_text = h.get_text().strip()
        if not h_id:
            h_id = slugify(h_text)
            h["id"] = h_id
        
        # Save for right sidebar TOC
        level = 2 if h.name == "h2" else 3
        toc_items.append({"id": h_id, "title": h_text, "level": level})
        
        # Add heading anchor link
        anchor = soup.new_tag("a", href=f"#{h_id}", **{"class": "header-anchor", "aria-label": f"Link to {h_text}"})
        anchor.string = "#"
        h.append(anchor)
        
    # Code block wrappers with copy button
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
        
    return str(soup), toc_items

def build_left_sidebar_html(current_url):
    nav_html = []
    for group in NAV_STRUCTURE:
        nav_html.append(f'<div class="sidebar-group">')
        nav_html.append(f'  <div class="sidebar-group-title">{group["group"]}</div>')
        nav_html.append(f'  <ul class="sidebar-group-items">')
        for item in group["items"]:
            is_active = (item.get("url") == current_url)
            active_cls = ' class="sidebar-link active" aria-current="page"' if is_active else ' class="sidebar-link"'
            if item.get("external"):
                nav_html.append(f'    <li><a href="{item["url"]}" target="_blank" rel="noopener noreferrer"{active_cls}><span>{item["title"]}</span> <span class="external-icon" aria-hidden="true">↗</span></a></li>')
            else:
                nav_html.append(f'    <li><a href="{item["url"]}"{active_cls}><span>{item["title"]}</span></a></li>')
        nav_html.append(f'  </ul>')
        nav_html.append(f'</div>')
    return "\n".join(nav_html)

def build_right_toc_html(toc_items):
    html = ['<div class="toc-container">', '  <div class="toc-title">ON THIS PAGE</div>', '  <nav class="toc-nav">', '    <ul class="toc-list">']
    html.append('      <li class="toc-item toc-level-2"><a href="#overview" class="toc-link" data-target="overview">Overview</a></li>')
    for item in toc_items:
        if item["id"] == "overview" or item["title"].lower() == "overview":
            continue
        lvl_cls = "toc-level-3" if item["level"] == 3 else "toc-level-2"
        html.append(f'      <li class="toc-item {lvl_cls}"><a href="#{item["id"]}" class="toc-link" data-target="{item["id"]}">{item["title"]}</a></li>')
    html.extend(['    </ul>', '  </nav>', '</div>'])
    return "\n".join(html)

def build_page_template(page_item, content_html, toc_items, group_title):
    current_url = page_item["url"]
    title = f"{page_item['title']} · Flint Docs" if page_item["url"] != "index.html" else "Flint — Offline Chat UI for Local Ollama Models"
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
    
    left_sidebar_html = build_left_sidebar_html(current_url)
    right_toc_html = build_right_toc_html(toc_items)
    
    return f'''<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{title}</title>
  <meta name="description" content="{description}">
  <link rel="icon" type="image/x-icon" href="favicon.ico">
  <link rel="stylesheet" href="assets/css/docs.css">
  <script>
    (function() {{
      try {{
        if (localStorage.getItem('flint_sidebar_collapsed') === 'true') {{
          document.documentElement.classList.add('sidebar-collapsed');
        }}
      }} catch (e) {{}}
    }})();
  </script>
</head>
<body data-page="{current_url}">
  <div class="docs-app">
    <!-- Top Navigation Bar -->
    <header class="top-nav" aria-label="Top Navigation">
      <div class="top-nav-left">
        <button class="icon-btn sidebar-toggle-btn" id="sidebarToggle" aria-label="Toggle navigation sidebar" title="Toggle sidebar (Ctrl+B)">
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <line x1="3" y1="12" x2="21" y2="12"></line>
            <line x1="3" y1="6" x2="21" y2="6"></line>
            <line x1="3" y1="18" x2="21" y2="18"></line>
          </svg>
        </button>
        <a href="index.html" class="brand-link">
          <img src="images/logo.png" alt="Flint logo" width="30" height="30" class="brand-logo">
          <span class="brand-name">Flint</span>
          <span class="version-pill">v0.2.0</span>
        </a>
      </div>

      <div class="top-nav-center">
        <button class="search-trigger" id="searchTrigger" aria-label="Search documentation (Press Ctrl+K)">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="11" cy="11" r="8"></circle>
            <line x1="21" y1="21" x2="16.65" y2="16.65"></line>
          </svg>
          <span class="search-text">Search docs...</span>
          <kbd class="search-kbd"><span class="kbd-cmd">Ctrl</span> K</kbd>
        </button>
      </div>

      <div class="top-nav-right">
        <nav class="top-nav-links" aria-label="Quick links">
          <a href="index.html" class="top-link{ ' active' if current_url in ('index.html', 'deployment.html') else '' }">Docs</a>
          <a href="features.html" class="top-link{ ' active' if current_url == 'features.html' else '' }">Features</a>
          <a href="architecture.html" class="top-link{ ' active' if current_url in ('architecture.html', 'context-management.html') else '' }">Architecture</a>
          <a href="changelog.html" class="top-link{ ' active' if current_url == 'changelog.html' else '' }">Changelog</a>
        </nav>
        <div class="top-nav-sep" aria-hidden="true"></div>
        <div class="top-nav-actions">
          <a href="https://github.com/Lusan-sapkota/Flint" class="icon-btn" aria-label="GitHub repository" target="_blank" rel="noopener noreferrer" title="View on GitHub">
            <svg width="20" height="20" viewBox="0 0 24 24" fill="currentColor">
              <path fill-rule="evenodd" clip-rule="evenodd" d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.53 1.032 1.53 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0112 6.844c.85.004 1.705.115 2.504.337 1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.019 10.019 0 0022 12.017C22 6.484 17.522 2 12 2z"/>
            </svg>
          </a>
          <a href="https://github.com/Lusan-sapkota/Flint/releases" class="icon-btn" aria-label="Releases" target="_blank" rel="noopener noreferrer" title="Releases">
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
              <polyline points="7 10 12 15 17 10"></polyline>
              <line x1="12" y1="15" x2="12" y2="3"></line>
            </svg>
          </a>
        </div>
      </div>
    </header>

    <!-- 3-Column Layout -->
    <div class="docs-body">
      <!-- Left Sidebar Navigation -->
      <aside class="left-sidebar" id="leftSidebar" aria-label="Documentation Sidebar">
        <nav class="sidebar-nav">
          {left_sidebar_html}
        </nav>
        <div class="sidebar-footer">
          <span class="sidebar-meta">Flint Documentation</span>
          <span class="sidebar-meta-sub">Zero build step · 100% offline</span>
        </div>
      </aside>

      <div class="sidebar-backdrop" id="sidebarBackdrop"></div>

      <!-- Center Main Reading Area -->
      <main class="center-content" id="mainContent">
        <div class="content-container">
          <!-- Breadcrumb and page action header -->
          <div class="content-top-meta">
            <div class="breadcrumb" aria-label="Breadcrumb">
              <span class="breadcrumb-group">{group_title.upper()}</span>
              <span class="breadcrumb-sep">›</span>
              <span class="breadcrumb-current">{page_item['title']}</span>
            </div>
            <button class="copy-page-btn" id="copyPageBtn" aria-label="Copy page URL" title="Copy link to page">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"></path>
                <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"></path>
              </svg>
              <span>Copy link</span>
            </button>
          </div>

          <!-- Main Article Content -->
          <article class="doc-article">
            {content_html}
          </article>

          {chr(10).join(prev_next_html)}

          <footer class="content-footer">
            <div class="footer-meta">
              <p>Flint is open source software licensed under <a href="https://github.com/Lusan-sapkota/Flint/blob/main/LICENSE" target="_blank" rel="noopener noreferrer">AGPL-3.0</a>.</p>
            </div>
            <a href="#mainContent" class="back-to-top" aria-label="Back to top">
              <span>Back to top</span>
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <polyline points="18 15 12 9 6 15"></polyline>
              </svg>
            </a>
          </footer>
        </div>
      </main>

      <!-- Right "On This Page" Table of Contents -->
      <aside class="right-toc" id="rightToc" aria-label="On this page navigation">
        {right_toc_html}
      </aside>
    </div>
  </div>

  <!-- Search Modal (Ctrl+K) -->
  <div class="search-modal-backdrop" id="searchModalBackdrop" style="display: none;">
    <div class="search-modal" id="searchModal" role="dialog" aria-modal="true" aria-label="Search Documentation">
      <div class="search-modal-header">
        <svg class="search-modal-icon" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <circle cx="11" cy="11" r="8"></circle>
          <line x1="21" y1="21" x2="16.65" y2="16.65"></line>
        </svg>
        <input type="text" class="search-modal-input" id="searchInput" placeholder="Search documentation, features, experiments..." autocomplete="off" spellcheck="false">
        <button class="search-modal-close" id="searchCloseBtn" aria-label="Close search">Esc</button>
      </div>
      <div class="search-modal-body">
        <div class="search-results" id="searchResults">
          <div class="search-empty">Type keywords to search across all Flint documentation...</div>
        </div>
      </div>
      <div class="search-modal-footer">
        <div class="search-hint"><span><kbd>↑</kbd> <kbd>↓</kbd> to navigate</span></div>
        <div class="search-hint"><span><kbd>↵</kbd> to select</span></div>
        <div class="search-hint"><span><kbd>esc</kbd> to close</span></div>
      </div>
    </div>
  </div>

  <script src="assets/js/search-index.js"></script>
  <script src="assets/js/docs.js"></script>
</body>
</html>
'''

def main():
    src_dir = "docs_backup"
    out_dir = "docs"
    
    if not os.path.exists(src_dir):
        print(f"Error: {src_dir} not found")
        return
        
    os.makedirs(out_dir, exist_ok=True)
    os.makedirs(os.path.join(out_dir, "assets", "css"), exist_ok=True)
    os.makedirs(os.path.join(out_dir, "assets", "js"), exist_ok=True)
    
    # Build search index data
    search_records = []
    
    for group in NAV_STRUCTURE:
        for item in group["items"]:
            if item.get("external"):
                continue
            src_path = os.path.join(src_dir, item["file"])
            if not os.path.exists(src_path):
                continue
            with open(src_path, "r", encoding="utf-8") as f:
                md_text = f.read()
                
            content_html, toc_items = convert_md_to_html(md_text)
            
            # Index page itself
            search_records.append({
                "page": item["title"],
                "url": item["url"],
                "title": item["title"],
                "group": group["group"],
                "snippet": item.get("desc", "")
            })
            
            # Index sections
            soup = BeautifulSoup(content_html, "html.parser")
            for h in soup.find_all(["h2", "h3"]):
                h_id = h.get("id")
                # find text until next heading or p
                p = h.find_next_sibling("p")
                snippet = p.get_text()[:140] if p else ""
                h_text = h.get_text().replace("#", "").strip()
                search_records.append({
                    "page": item["title"],
                    "url": f"{item['url']}#{h_id}",
                    "title": h_text,
                    "group": group["group"],
                    "snippet": snippet
                })
                
            full_html = build_page_template(item, content_html, toc_items, group["group"])
            out_path = os.path.join(out_dir, item["url"])
            with open(out_path, "w", encoding="utf-8") as f:
                f.write(full_html)
            print(f"Rendered {item['file']} -> {item['url']} ({len(toc_items)} TOC items)")
            
    # Write search index JS
    search_js_path = os.path.join(out_dir, "assets", "js", "search-index.js")
    with open(search_js_path, "w", encoding="utf-8") as f:
        f.write("window.FLINT_SEARCH_INDEX = " + json.dumps(search_records, indent=2) + ";\n")
    print(f"Generated search index with {len(search_records)} entries")

    # Copy changelog.md to docs/changelog.md for maintainer awk release script
    shutil.copy2(os.path.join(src_dir, "changelog.md"), os.path.join(out_dir, "changelog.md"))
    print("Preserved docs/changelog.md for maintainer release workflow")

if __name__ == "__main__":
    main()
