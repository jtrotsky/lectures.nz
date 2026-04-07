/**
 * lectures.nz — client-side search, location filter & URL sync
 *
 * Pure vanilla JS, no dependencies.
 * Lectures data is embedded in the page as window.LECTURES_DATA (array of lecture objects).
 */

(function () {
  'use strict';

  const searchInput = document.getElementById('search');
  const lectures = window.LECTURES_DATA || [];

  // ---- Known NZ cities (used for location matching & detection) ----------

  const NZ_CITIES = ['Auckland', 'Wellington', 'Christchurch', 'Dunedin', 'Hamilton', 'Tauranga', 'Nelson', 'Napier', 'Palmerston North'];

  // ---- Location state ---------------------------------------------------

  const CITY_KEY = 'lectures_city';
  let activeCity = localStorage.getItem(CITY_KEY) || null;

  function extractCity(locationStr) {
    if (!locationStr) return null;
    for (const city of NZ_CITIES) {
      if (locationStr.includes(city)) return city;
    }
    return null;
  }

  function setCity(city) {
    activeCity = city || null;
    if (activeCity) {
      localStorage.setItem(CITY_KEY, activeCity);
    } else {
      localStorage.removeItem(CITY_KEY);
    }
    renderCityPill();
    applyFilter(currentQuery);
  }

  function renderCityPill() {
    const pill = document.getElementById('city-pill');
    if (!pill) return;
    if (activeCity) {
      pill.textContent = '📍 ' + activeCity;
      pill.classList.add('city-pill--active');
      pill.title = 'Showing ' + activeCity + ' lectures — click to change';
    } else {
      pill.textContent = '📍 All cities';
      pill.classList.remove('city-pill--active');
      pill.title = 'Click to filter by city';
    }
  }

  function showCityPicker() {
    const existing = document.getElementById('city-picker');
    if (existing) { existing.remove(); return; }

    const picker = document.createElement('div');
    picker.id = 'city-picker';
    picker.className = 'city-picker';

    const header = document.createElement('div');
    header.className = 'city-picker-header';
    header.textContent = 'Filter by city';
    picker.appendChild(header);

    const allBtn = document.createElement('button');
    allBtn.className = 'city-picker-option' + (!activeCity ? ' city-picker-option--active' : '');
    allBtn.textContent = 'All cities';
    allBtn.onclick = function () { setCity(null); picker.remove(); };
    picker.appendChild(allBtn);

    // Only show cities that have lectures
    const availableCities = new Set();
    for (const l of lectures) {
      const c = extractCity(l.location || '');
      if (c) availableCities.add(c);
    }

    for (const city of NZ_CITIES) {
      if (!availableCities.has(city)) continue;
      const btn = document.createElement('button');
      btn.className = 'city-picker-option' + (activeCity === city ? ' city-picker-option--active' : '');
      btn.textContent = city;
      btn.onclick = function () { setCity(city); picker.remove(); };
      picker.appendChild(btn);
    }

    const pill = document.getElementById('city-pill');
    if (pill) {
      pill.parentNode.insertBefore(picker, pill.nextSibling);
    }

    // Close on outside click
    setTimeout(function () {
      document.addEventListener('click', function close(e) {
        if (!picker.contains(e.target) && e.target !== pill) {
          picker.remove();
          document.removeEventListener('click', close);
        }
      });
    }, 0);
  }

  // ---- IP-based city detection (runs once if no city stored) ------------

  function detectCity() {
    if (activeCity) return; // already set
    fetch('https://ipapi.co/json/')
      .then(function (r) { return r.json(); })
      .then(function (data) {
        const city = data.city || '';
        // Only auto-set if it's a recognised NZ city
        if (NZ_CITIES.includes(city)) {
          setCity(city);
        }
      })
      .catch(function () { /* silently ignore — user can set manually */ });
  }

  // ---- Fuzzy search -----------------------------------------------------

  function fuzzyScore(haystack, needle) {
    if (!needle) return 1;
    haystack = haystack.toLowerCase();
    needle = needle.toLowerCase();
    if (haystack.includes(needle)) return 100;
    let hi = 0, ni = 0, score = 0;
    while (hi < haystack.length && ni < needle.length) {
      if (haystack[hi] === needle[ni]) { score++; ni++; }
      hi++;
    }
    if (ni < needle.length) return 0;
    return score;
  }

  function scoreLecture(lecture, query) {
    if (!query) return 1;
    const terms = query.trim().split(/\s+/).filter(Boolean);
    let total = 0;
    for (const term of terms) {
      const titleScore    = fuzzyScore(lecture.title || '', term) * 10;
      const speakerScore  = (lecture.speakers || []).reduce(function (acc, s) { return acc + fuzzyScore(s.name || '', term); }, 0) * 8;
      const locationScore = fuzzyScore(lecture.location || '', term) * 5;
      const summaryScore  = fuzzyScore(lecture.summary || '', term) * 2;
      const hostScore     = fuzzyScore(lecture.host_slug || '', term) * 3;
      const best = Math.max(titleScore, speakerScore, locationScore, summaryScore, hostScore);
      if (best === 0) return 0;
      total += best;
    }
    return total;
  }

  // ---- Filter logic (search + city) ------------------------------------

  let currentQuery = '';

  function applyFilter(query) {
    currentQuery = query;
    const q = query.trim();

    const lectureMap = {};
    for (const l of lectures) { lectureMap[l.id] = l; }

    const items = document.querySelectorAll('.lecture-item');
    let visibleCount = 0;

    items.forEach(function (item) {
      const id = item.dataset.id;
      const lecture = lectureMap[id];
      let show = true;

      if (q && lecture) {
        show = scoreLecture(lecture, q) > 0;
      }

      if (show && activeCity && lecture) {
        const itemCity = extractCity(lecture.location || '');
        show = itemCity === activeCity;
      }

      item.hidden = !show;
      if (show) visibleCount++;
    });

    // Hide empty date groups
    document.querySelectorAll('.date-group').forEach(function (group) {
      group.hidden = group.querySelectorAll('.lecture-item:not([hidden])').length === 0;
    });

    // No-results message
    const noResults = document.getElementById('no-results');
    if (noResults) {
      const empty = visibleCount === 0 && (q || activeCity);
      noResults.hidden = !empty;
      if (empty) {
        const qSpan = document.getElementById('no-results-query');
        if (qSpan) qSpan.textContent = q || activeCity || '';
      }
    }
  }

  // ---- URL query param sync --------------------------------------------

  function getQueryFromURL() {
    try { return new URLSearchParams(window.location.search).get('q') || ''; }
    catch (e) { return ''; }
  }

  function setQueryInURL(q) {
    try {
      const url = new URL(window.location.href);
      if (q) { url.searchParams.set('q', q); } else { url.searchParams.delete('q'); }
      window.history.replaceState(null, '', url.toString());
    } catch (e) { /* ignore */ }
  }

  // ---- Event wiring ----------------------------------------------------

  if (searchInput) {
    let debounceTimer = null;
    searchInput.addEventListener('input', function () {
      clearTimeout(debounceTimer);
      debounceTimer = setTimeout(function () {
        const q = searchInput.value;
        applyFilter(q);
        setQueryInURL(q);
      }, 120);
    });

    searchInput.addEventListener('keydown', function (e) {
      if (e.key === 'Escape') {
        searchInput.value = '';
        applyFilter('');
        setQueryInURL('');
        searchInput.blur();
      }
    });

    document.addEventListener('keydown', function (e) {
      if (e.key === '/' && document.activeElement !== searchInput) {
        e.preventDefault();
        searchInput.focus();
      }
    });
  }

  const cityPill = document.getElementById('city-pill');
  if (cityPill) {
    cityPill.addEventListener('click', showCityPicker);
  }

  window.clearSearch = function () {
    if (searchInput) searchInput.value = '';
    applyFilter('');
    setQueryInURL('');
  };

  // ---- Init ------------------------------------------------------------

  renderCityPill();
  detectCity();

  const initialQuery = getQueryFromURL();
  if (initialQuery && searchInput) {
    searchInput.value = initialQuery;
  }
  applyFilter(initialQuery);

})();
