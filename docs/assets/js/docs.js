// Flint Documentation Client JS - Zero dependencies

document.addEventListener('DOMContentLoaded', () => {
  const layout = document.querySelector('.docs-layout');
  const sidebar = document.getElementById('sidebar');
  const backdrop = document.getElementById('sidebarBackdrop');
  const menuToggle = document.getElementById('menuToggle');
  const sidebarCloseBtn = document.getElementById('sidebarCloseBtn');
  const sidebarCollapseBtn = document.getElementById('sidebarCollapseBtn');
  const sidebarExpandBtn = document.getElementById('sidebarExpandBtn');

  // =========================================================================
  // 1. Desktop Sidebar Collapse / Expand with LocalStorage Persistence
  // =========================================================================
  const STORAGE_KEY = 'flint_docs_sidebar_collapsed';

  function isCollapsed() {
    return layout ? layout.classList.contains('sidebar-collapsed') : false;
  }

  function setCollapsed(collapsed) {
    if (!layout) return;
    if (collapsed) {
      layout.classList.add('sidebar-collapsed');
      document.documentElement.classList.add('sidebar-collapsed-pre');
      try { localStorage.setItem(STORAGE_KEY, 'true'); } catch (e) {}
    } else {
      layout.classList.remove('sidebar-collapsed');
      document.documentElement.classList.remove('sidebar-collapsed-pre');
      try { localStorage.setItem(STORAGE_KEY, 'false'); } catch (e) {}
    }
  }

  // Restore stored state on load
  try {
    const savedState = localStorage.getItem(STORAGE_KEY);
    if (savedState === 'true') {
      setCollapsed(true);
    } else if (savedState === 'false') {
      setCollapsed(false);
    }
  } catch (e) {}

  if (sidebarCollapseBtn) {
    sidebarCollapseBtn.addEventListener('click', () => {
      setCollapsed(true);
    });
  }

  if (sidebarExpandBtn) {
    sidebarExpandBtn.addEventListener('click', () => {
      setCollapsed(false);
    });
  }

  // Keyboard shortcut: Ctrl+B or Cmd+B to toggle sidebar on desktop
  document.addEventListener('keydown', (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'b') {
      // Only handle if desktop (width > 880px)
      if (window.innerWidth > 880) {
        e.preventDefault();
        setCollapsed(!isCollapsed());
      }
    }
  });

  // =========================================================================
  // 2. Mobile Drawer Navigation (Mobile & Tablet <= 880px)
  // =========================================================================
  function openMobileSidebar() {
    if (sidebar && backdrop && menuToggle) {
      sidebar.classList.add('open');
      backdrop.classList.add('active');
      menuToggle.setAttribute('aria-expanded', 'true');
      document.body.style.overflow = 'hidden';
    }
  }

  function closeMobileSidebar() {
    if (sidebar && backdrop && menuToggle) {
      sidebar.classList.remove('open');
      backdrop.classList.remove('active');
      menuToggle.setAttribute('aria-expanded', 'false');
      document.body.style.overflow = '';
    }
  }

  if (menuToggle) {
    menuToggle.addEventListener('click', () => {
      const isOpen = sidebar.classList.contains('open');
      if (isOpen) {
        closeMobileSidebar();
      } else {
        openMobileSidebar();
      }
    });
  }

  if (sidebarCloseBtn) {
    sidebarCloseBtn.addEventListener('click', closeMobileSidebar);
  }

  if (backdrop) {
    backdrop.addEventListener('click', closeMobileSidebar);
  }

  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      closeMobileSidebar();
    }
  });

  // Close mobile sidebar when clicking any navigation link
  const sidebarLinks = document.querySelectorAll('.docs-sidebar a');
  sidebarLinks.forEach(link => {
    link.addEventListener('click', () => {
      if (window.innerWidth <= 880) {
        closeMobileSidebar();
      }
    });
  });

  // =========================================================================
  // 3. Code Block Copy Buttons
  // =========================================================================
  const copyButtons = document.querySelectorAll('.copy-code-btn');
  copyButtons.forEach(btn => {
    btn.addEventListener('click', async () => {
      const codeBlock = btn.closest('.code-block');
      if (!codeBlock) return;
      const pre = codeBlock.querySelector('pre');
      if (!pre) return;

      const codeText = pre.innerText;
      try {
        await navigator.clipboard.writeText(codeText);
        btn.textContent = 'Copied!';
        btn.classList.add('copied');
        setTimeout(() => {
          btn.textContent = 'Copy';
          btn.classList.remove('copied');
        }, 2000);
      } catch (err) {
        // Fallback for older browsers
        const textarea = document.createElement('textarea');
        textarea.value = codeText;
        textarea.style.position = 'fixed';
        textarea.style.opacity = '0';
        document.body.appendChild(textarea);
        textarea.select();
        try {
          document.execCommand('copy');
          btn.textContent = 'Copied!';
          btn.classList.add('copied');
          setTimeout(() => {
            btn.textContent = 'Copy';
            btn.classList.remove('copied');
          }, 2000);
        } catch (e) {
          btn.textContent = 'Failed';
          setTimeout(() => {
            btn.textContent = 'Copy';
          }, 2000);
        }
        document.body.removeChild(textarea);
      }
    });
  });

  // =========================================================================
  // 4. Scroll Active Navigation Item Into View
  // =========================================================================
  const activeNavItem = document.querySelector('.nav-item.active');
  if (activeNavItem && sidebar) {
    const rect = activeNavItem.getBoundingClientRect();
    const sidebarRect = sidebar.getBoundingClientRect();
    if (rect.top < sidebarRect.top || rect.bottom > sidebarRect.bottom) {
      activeNavItem.scrollIntoView({ block: 'center', behavior: 'smooth' });
    }
  }
});
