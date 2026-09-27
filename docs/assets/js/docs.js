// Flint Documentation Client JS - Zero dependencies

document.addEventListener('DOMContentLoaded', () => {
  // 1. Mobile Menu Drawer Toggle
  const menuToggle = document.getElementById('menuToggle');
  const sidebar = document.getElementById('sidebar');
  const backdrop = document.getElementById('sidebarBackdrop');

  function openSidebar() {
    if (sidebar && backdrop && menuToggle) {
      sidebar.classList.add('open');
      backdrop.classList.add('active');
      menuToggle.setAttribute('aria-expanded', 'true');
      document.body.style.overflow = 'hidden';
    }
  }

  function closeSidebar() {
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
        closeSidebar();
      } else {
        openSidebar();
      }
    });
  }

  if (backdrop) {
    backdrop.addEventListener('click', closeSidebar);
  }

  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      closeSidebar();
    }
  });

  // Close mobile sidebar when clicking a link
  const sidebarLinks = document.querySelectorAll('.docs-sidebar a');
  sidebarLinks.forEach(link => {
    link.addEventListener('click', () => {
      if (window.innerWidth <= 880) {
        closeSidebar();
      }
    });
  });

  // 2. Code Block Copy Buttons
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
        // Fallback for older browsers or insecure origins
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

  // 3. Scroll active sidebar item into view if not visible
  const activeNavItem = document.querySelector('.nav-item.active');
  if (activeNavItem && sidebar) {
    const rect = activeNavItem.getBoundingClientRect();
    const sidebarRect = sidebar.getBoundingClientRect();
    if (rect.top < sidebarRect.top || rect.bottom > sidebarRect.bottom) {
      activeNavItem.scrollIntoView({ block: 'center', behavior: 'smooth' });
    }
  }
});
