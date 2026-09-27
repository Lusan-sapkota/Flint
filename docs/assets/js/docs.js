// Flint Documentation Client JS - Zero external dependencies

document.addEventListener('DOMContentLoaded', () => {
  // =========================================================================
  // 1. DOM Elements
  // =========================================================================
  const sidebar = document.getElementById('leftSidebar');
  const sidebarToggle = document.getElementById('sidebarToggle');
  const sidebarBackdrop = document.getElementById('sidebarBackdrop');
  
  const searchTrigger = document.getElementById('searchTrigger');
  const searchModalBackdrop = document.getElementById('searchModalBackdrop');
  const searchModal = document.getElementById('searchModal');
  const searchInput = document.getElementById('searchInput');
  const searchCloseBtn = document.getElementById('searchCloseBtn');
  const searchResults = document.getElementById('searchResults');
  
  const copyPageBtn = document.getElementById('copyPageBtn');
  const rightToc = document.getElementById('rightToc');
  const tocLinks = Array.from(document.querySelectorAll('.toc-link'));
  const articleHeadings = Array.from(document.querySelectorAll('.doc-article h1[id], .doc-article h2[id], .doc-article h3[id]'));

  // =========================================================================
  // 2. Mobile & Tablet Drawer Sidebar Navigation
  // =========================================================================
  function openMobileSidebar() {
    if (sidebar && sidebarBackdrop) {
      sidebar.classList.add('open');
      sidebarBackdrop.classList.add('active');
      document.body.style.overflow = 'hidden';
    }
  }

  function closeMobileSidebar() {
    if (sidebar && sidebarBackdrop) {
      sidebar.classList.remove('open');
      sidebarBackdrop.classList.remove('active');
      document.body.style.overflow = '';
    }
  }

  if (sidebarToggle) {
    sidebarToggle.addEventListener('click', (e) => {
      e.stopPropagation();
      if (sidebar && sidebar.classList.contains('open')) {
        closeMobileSidebar();
      } else {
        openMobileSidebar();
      }
    });
  }

  if (sidebarBackdrop) {
    sidebarBackdrop.addEventListener('click', closeMobileSidebar);
  }

  // Close mobile sidebar on link click
  const navLinks = document.querySelectorAll('.sidebar-link');
  navLinks.forEach(link => {
    link.addEventListener('click', () => {
      if (window.innerWidth <= 960) {
        closeMobileSidebar();
      }
    });
  });

  // Desktop sidebar collapse shortcut (Ctrl+B / Cmd+B)
  const STORAGE_KEY = 'flint_sidebar_collapsed';
  function toggleDesktopSidebar() {
    const isCollapsed = document.documentElement.classList.toggle('sidebar-collapsed');
    try {
      localStorage.setItem(STORAGE_KEY, isCollapsed ? 'true' : 'false');
    } catch (e) {}
  }

  document.addEventListener('keydown', (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'b') {
      if (window.innerWidth > 960) {
        e.preventDefault();
        toggleDesktopSidebar();
      }
    }
  });

  // Scroll active sidebar link into view
  const activeSidebarLink = document.querySelector('.sidebar-link.active');
  if (activeSidebarLink && sidebar) {
    const rect = activeSidebarLink.getBoundingClientRect();
    const sidebarRect = sidebar.getBoundingClientRect();
    if (rect.top < sidebarRect.top || rect.bottom > sidebarRect.bottom) {
      activeSidebarLink.scrollIntoView({ block: 'center' });
    }
  }

  // =========================================================================
  // 3. ScrollSpy & Smooth Anchor Scrolling for "ON THIS PAGE" TOC
  // =========================================================================
  const TOP_NAV_HEIGHT = 60;
  let activeTocLink = null;
  let isThrottled = false;

  function updateScrollSpy() {
    if (articleHeadings.length === 0 || tocLinks.length === 0) return;

    const scrollY = window.scrollY || window.pageYOffset;
    const windowHeight = window.innerHeight;
    const docHeight = document.documentElement.scrollHeight;

    // 1. If at the very top of the page, activate "overview" (or first link)
    if (scrollY < 120) {
      setActiveTocLink(tocLinks[0]);
      return;
    }

    // 2. If at the bottom of the page, activate the last link
    if (scrollY + windowHeight >= docHeight - 50) {
      setActiveTocLink(tocLinks[tocLinks.length - 1]);
      return;
    }

    // 3. Find the heading currently above the offset line
    const offset = TOP_NAV_HEIGHT + 40;
    let currentHeading = null;

    for (let i = 0; i < articleHeadings.length; i++) {
      const heading = articleHeadings[i];
      const top = heading.getBoundingClientRect().top;
      if (top <= offset) {
        currentHeading = heading;
      } else {
        break;
      }
    }

    if (!currentHeading && articleHeadings.length > 0) {
      currentHeading = articleHeadings[0];
    }

    if (currentHeading) {
      const targetId = currentHeading.getAttribute('id');
      const matchingLink = tocLinks.find(link => link.getAttribute('data-target') === targetId);
      if (matchingLink) {
        setActiveTocLink(matchingLink);
      }
    }
  }

  function setActiveTocLink(link) {
    if (!link || link === activeTocLink) return;
    if (activeTocLink) {
      activeTocLink.classList.remove('active');
    }
    link.classList.add('active');
    activeTocLink = link;

    // Scroll active item into view inside right TOC if needed
    if (rightToc) {
      const containerRect = rightToc.getBoundingClientRect();
      const linkRect = link.getBoundingClientRect();
      if (linkRect.top < containerRect.top || linkRect.bottom > containerRect.bottom) {
        link.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
      }
    }
  }

  window.addEventListener('scroll', () => {
    if (!isThrottled) {
      window.requestAnimationFrame(() => {
        updateScrollSpy();
        isThrottled = false;
      });
      isThrottled = true;
    }
  }, { passive: true });

  // Initial scrollspy run
  updateScrollSpy();

  // Smooth TOC Link clicks with header offset
  tocLinks.forEach(link => {
    link.addEventListener('click', (e) => {
      const targetId = link.getAttribute('data-target');
      if (!targetId) return;

      const targetEl = document.getElementById(targetId);
      if (targetEl) {
        e.preventDefault();
        const y = targetEl.getBoundingClientRect().top + window.pageYOffset - TOP_NAV_HEIGHT - 16;
        window.scrollTo({ top: Math.max(0, y), behavior: 'smooth' });
        history.pushState(null, null, `#${targetId}`);
        setActiveTocLink(link);
      }
    });
  });

  // Smooth Header Anchor clicks
  const headerAnchors = document.querySelectorAll('.header-anchor');
  headerAnchors.forEach(anchor => {
    anchor.addEventListener('click', (e) => {
      const href = anchor.getAttribute('href');
      if (href && href.startsWith('#')) {
        const targetId = href.substring(1);
        const targetEl = document.getElementById(targetId);
        if (targetEl) {
          e.preventDefault();
          const y = targetEl.getBoundingClientRect().top + window.pageYOffset - TOP_NAV_HEIGHT - 16;
          window.scrollTo({ top: Math.max(0, y), behavior: 'smooth' });
          history.pushState(null, null, href);
        }
      }
    });
  });

  // =========================================================================
  // 4. Instant Offline Search Modal (Ctrl+K)
  // =========================================================================
  let selectedSearchIndex = 0;
  let currentSearchResults = [];

  function openSearchModal() {
    if (!searchModalBackdrop) return;
    searchModalBackdrop.style.display = 'flex';
    document.body.style.overflow = 'hidden';
    if (searchInput) {
      searchInput.value = '';
      searchInput.focus();
    }
    renderSearchResults('');
  }

  function closeSearchModal() {
    if (!searchModalBackdrop) return;
    searchModalBackdrop.style.display = 'none';
    document.body.style.overflow = '';
  }

  if (searchTrigger) {
    searchTrigger.addEventListener('click', openSearchModal);
  }

  if (searchCloseBtn) {
    searchCloseBtn.addEventListener('click', closeSearchModal);
  }

  if (searchModalBackdrop) {
    searchModalBackdrop.addEventListener('click', (e) => {
      if (e.target === searchModalBackdrop) {
        closeSearchModal();
      }
    });
  }

  // Keyboard shortcut listener (Ctrl+K, Cmd+K, /, Escape)
  document.addEventListener('keydown', (e) => {
    // Open Search: Ctrl+K or Cmd+K
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
      e.preventDefault();
      if (searchModalBackdrop && searchModalBackdrop.style.display === 'flex') {
        closeSearchModal();
      } else {
        openSearchModal();
      }
      return;
    }

    // Open Search: '/' key when not typing in an input/textarea
    if (e.key === '/' && !['INPUT', 'TEXTAREA'].includes(document.activeElement.tagName) && !document.activeElement.isContentEditable) {
      e.preventDefault();
      openSearchModal();
      return;
    }

    // Escape closes search or mobile sidebar
    if (e.key === 'Escape') {
      if (searchModalBackdrop && searchModalBackdrop.style.display === 'flex') {
        closeSearchModal();
        return;
      }
      closeMobileSidebar();
    }
  });

  function escapeHtml(str) {
    return (str || '')
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  function highlightMatches(text, query) {
    if (!text || !query) return escapeHtml(text);
    const escapedText = escapeHtml(text);
    const words = query.trim().split(/\s+/).filter(w => w.length > 0);
    if (words.length === 0) return escapedText;

    const regex = new RegExp(`(${words.map(w => w.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|')})`, 'gi');
    return escapedText.replace(regex, '<span style="color:var(--flint-accent);font-weight:600;text-decoration:underline;">$1</span>');
  }

  function renderSearchResults(query) {
    if (!searchResults) return;

    const trimmed = query.trim().toLowerCase();
    const index = window.FLINT_SEARCH_INDEX || [];

    if (!trimmed) {
      searchResults.innerHTML = '<div class="search-empty">Type keywords to search across all Flint documentation...</div>';
      currentSearchResults = [];
      selectedSearchIndex = 0;
      return;
    }

    const words = trimmed.split(/\s+/).filter(Boolean);

    // Score and rank matches
    const scored = [];
    for (const item of index) {
      let score = 0;
      const titleLower = (item.title || '').toLowerCase();
      const snippetLower = (item.snippet || '').toLowerCase();
      const pageLower = (item.page || '').toLowerCase();

      // Exact title match
      if (titleLower === trimmed) {
        score += 150;
      } else if (titleLower.startsWith(trimmed)) {
        score += 80;
      }

      let allWordsFound = true;
      for (const w of words) {
        let wordFound = false;
        if (titleLower.includes(w)) {
          score += 35;
          wordFound = true;
        }
        if (pageLower.includes(w)) {
          score += 20;
          wordFound = true;
        }
        if (snippetLower.includes(w)) {
          score += 10;
          wordFound = true;
        }
        if (!wordFound) {
          allWordsFound = false;
        }
      }

      if (allWordsFound && words.length > 1) {
        score += 40;
      }

      if (score > 0) {
        scored.push({ item, score });
      }
    }

    scored.sort((a, b) => b.score - a.score);
    currentSearchResults = scored.slice(0, 15).map(s => s.item);
    selectedSearchIndex = 0;

    if (currentSearchResults.length === 0) {
      searchResults.innerHTML = `<div class="search-empty">No results found for "<strong>${escapeHtml(trimmed)}</strong>"</div>`;
      return;
    }

    let html = '';
    currentSearchResults.forEach((item, idx) => {
      const isSelected = (idx === selectedSearchIndex);
      const selClass = isSelected ? ' selected' : '';
      html += `
        <a href="${escapeHtml(item.url)}" class="search-item${selClass}" data-index="${idx}">
          <div class="search-item-header">
            <span class="search-item-title">${highlightMatches(item.title, trimmed)}</span>
            <span class="search-item-group">${escapeHtml(item.group || item.page)}</span>
          </div>
          ${item.snippet ? `<div class="search-item-snippet">${highlightMatches(item.snippet, trimmed)}</div>` : ''}
        </a>
      `;
    });

    searchResults.innerHTML = html;

    // Attach click and hover handlers to result items
    const renderedItems = searchResults.querySelectorAll('.search-item');
    renderedItems.forEach(elem => {
      elem.addEventListener('mouseenter', () => {
        const idx = parseInt(elem.getAttribute('data-index'), 10);
        updateSearchSelection(idx);
      });
      elem.addEventListener('click', () => {
        closeSearchModal();
      });
    });
  }

  function updateSearchSelection(newIndex) {
    if (currentSearchResults.length === 0) return;
    const items = searchResults.querySelectorAll('.search-item');
    if (selectedSearchIndex >= 0 && selectedSearchIndex < items.length) {
      items[selectedSearchIndex].classList.remove('selected');
    }
    selectedSearchIndex = newIndex;
    if (selectedSearchIndex >= 0 && selectedSearchIndex < items.length) {
      items[selectedSearchIndex].classList.add('selected');
      items[selectedSearchIndex].scrollIntoView({ block: 'nearest' });
    }
  }

  if (searchInput) {
    searchInput.addEventListener('input', (e) => {
      renderSearchResults(e.target.value);
    });

    searchInput.addEventListener('keydown', (e) => {
      if (currentSearchResults.length === 0) return;

      if (e.key === 'ArrowDown') {
        e.preventDefault();
        const nextIndex = (selectedSearchIndex + 1) % currentSearchResults.length;
        updateSearchSelection(nextIndex);
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        const prevIndex = (selectedSearchIndex - 1 + currentSearchResults.length) % currentSearchResults.length;
        updateSearchSelection(prevIndex);
      } else if (e.key === 'Enter') {
        e.preventDefault();
        if (currentSearchResults[selectedSearchIndex]) {
          const targetUrl = currentSearchResults[selectedSearchIndex].url;
          closeSearchModal();
          window.location.href = targetUrl;
        }
      }
    });
  }

  // =========================================================================
  // 5. Code Block Copy Buttons
  // =========================================================================
  const copyButtons = document.querySelectorAll('.copy-code-btn');
  copyButtons.forEach(btn => {
    btn.addEventListener('click', async () => {
      const codeBlock = btn.closest('.code-block');
      if (!codeBlock) return;
      const pre = codeBlock.querySelector('pre');
      if (!pre) return;

      const codeText = pre.innerText;
      await copyTextToClipboard(codeText);
      showButtonCopied(btn, 'Copied!');
    });
  });

  // =========================================================================
  // 6. Copy Page URL Button
  // =========================================================================
  if (copyPageBtn) {
    copyPageBtn.addEventListener('click', async () => {
      await copyTextToClipboard(window.location.href);
      const span = copyPageBtn.querySelector('span');
      const originalText = span ? span.textContent : 'Copy link';
      copyPageBtn.classList.add('copied');
      if (span) span.textContent = 'Copied!';
      setTimeout(() => {
        copyPageBtn.classList.remove('copied');
        if (span) span.textContent = originalText;
      }, 2000);
    });
  }

  async function copyTextToClipboard(text) {
    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(text);
        return;
      }
    } catch (e) {}

    // Fallback using textarea
    const textarea = document.createElement('textarea');
    textarea.value = text;
    textarea.style.position = 'fixed';
    textarea.style.left = '-9999px';
    textarea.style.top = '0';
    textarea.style.opacity = '0';
    document.body.appendChild(textarea);
    textarea.focus();
    textarea.select();
    try {
      document.execCommand('copy');
    } catch (err) {}
    document.body.removeChild(textarea);
  }

  function showButtonCopied(button, message) {
    const originalText = button.textContent;
    button.textContent = message;
    button.classList.add('copied');
    setTimeout(() => {
      button.textContent = originalText;
      button.classList.remove('copied');
    }, 2000);
  }
});
