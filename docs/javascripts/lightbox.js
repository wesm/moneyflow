/**
 * Simple image lightbox for documentation screenshots
 * Works on all screen sizes (desktop, tablet, mobile)
 */

(function() {
  'use strict';

  // Create lightbox elements
  function createLightbox() {
    const overlay = document.createElement('div');
    overlay.id = 'lightbox-overlay';
    overlay.innerHTML = `
      <div class="lightbox-content">
        <div id="lightbox-image-container"></div>
        <button id="lightbox-close" aria-label="Close lightbox">&times;</button>
        <div class="lightbox-caption"></div>
      </div>
    `;
    document.body.appendChild(overlay);

    // Close on overlay click (not on image or caption)
    overlay.addEventListener('click', (e) => {
      if (e.target === overlay || e.target.id === 'lightbox-close') {
        closeLightbox();
      }
    });

    // Close on Escape key
    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && overlay.classList.contains('active')) {
        closeLightbox();
      }
    });

    // Support touch gestures for mobile
    let touchStartY = 0;
    overlay.addEventListener('touchstart', (e) => {
      touchStartY = e.touches[0].clientY;
    });

    overlay.addEventListener('touchend', (e) => {
      const touchEndY = e.changedTouches[0].clientY;
      const swipeDistance = touchStartY - touchEndY;

      // Swipe down to close (must swipe at least 100px)
      if (swipeDistance < -100) {
        closeLightbox();
      }
    });

    return overlay;
  }

  // Open lightbox with image
  function openLightbox(imgSrc, altText) {
    const overlay = document.getElementById('lightbox-overlay') || createLightbox();
    const container = document.getElementById('lightbox-image-container');
    const caption = overlay.querySelector('.lightbox-caption');

    // Clear previous content
    container.innerHTML = '';

    // Check if it's an SVG
    if (imgSrc.endsWith('.svg')) {
      // For SVG, create an img element with specific sizing
      const img = document.createElement('img');
      img.src = imgSrc;
      img.alt = altText;
      img.id = 'lightbox-image';
      img.style.width = 'auto';
      img.style.height = 'auto';
      img.style.maxWidth = '100%';
      img.style.maxHeight = '100%';
      container.appendChild(img);
    } else {
      // For PNG/JPG, use regular img tag
      const img = document.createElement('img');
      img.src = imgSrc;
      img.alt = altText;
      img.id = 'lightbox-image';
      container.appendChild(img);
    }

    caption.textContent = altText;

    overlay.classList.add('active');
    document.body.style.overflow = 'hidden'; // Prevent scrolling
  }

  // Close lightbox
  function closeLightbox() {
    const overlay = document.getElementById('lightbox-overlay');
    if (overlay) {
      overlay.classList.remove('active');
      document.body.style.overflow = ''; // Restore scrolling
    }
  }

  // Make images clickable
  function initLightbox() {
    // Target all content images (screenshots)
    const images = document.querySelectorAll('.md-content img');

    images.forEach(img => {
      // Skip if already processed
      if (img.classList.contains('lightbox-enabled')) return;

      // Skip badges/shields (shields.io, badge URLs)
      if (img.src.includes('shields.io') || img.src.includes('badge')) {
        return;
      }

      // Add class and click handler
      img.classList.add('lightbox-enabled');
      img.title = img.alt + ' (click to enlarge)';

      img.addEventListener('click', (e) => {
        e.preventDefault();
        openLightbox(img.src, img.alt);
      });
    });
  }

  // Initialize on page load
  document.addEventListener('DOMContentLoaded', initLightbox);

  // Re-initialize on Material for MkDocs instant navigation
  if (typeof document$ !== 'undefined') {
    document$.subscribe(() => {
      initLightbox();
    });
  }
})();
