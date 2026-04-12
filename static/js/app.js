/**
 * lectures.nz — topic filter, city filter & URL sync
 *
 * Pure vanilla JS, no dependencies.
 * Lectures data is embedded in the page as window.LECTURES_DATA (array of lecture objects).
 */

(function () {
  'use strict';

  const lectures = window.LECTURES_DATA || [];

  // ---- Known NZ cities -------------------------------------------------

  const NZ_CITIES = ['Auckland', 'Wellington', 'Christchurch', 'Dunedin', 'Hamilton', 'Tauranga', 'Nelson', 'Napier', 'Palmerston North'];

  // ---- Topic filter state ----------------------------------------------

  let activeTopic = null;

  function setTopic(slug) {
    activeTopic = slug;
    document.querySelectorAll('.topic-chip').forEach(function (btn) {
      btn.classList.toggle('topic-chip--active', btn.dataset.topic === slug);
    });
    applyFilter();
    setTopicInURL(slug);
  }

  const topicChips = document.getElementById('topic-chips');
  if (topicChips) {
    topicChips.addEventListener('click', function (e) {
      const btn = e.target.closest('.topic-chip');
      if (!btn) return;
      setTopic(activeTopic === btn.dataset.topic ? null : btn.dataset.topic);
    });
  }

  // ---- Location state --------------------------------------------------

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
    applyFilter();
  }

  function renderCityPill() {
    const pill = document.getElementById('city-pill');
    if (!pill) return;
    var label = pill.querySelector('.city-pill-label');
    if (!label) {
      label = document.createElement('span');
      label.className = 'city-pill-label';
      pill.appendChild(label);
    }
    if (activeCity) {
      label.textContent = activeCity;
      pill.classList.add('city-pill--active');
      pill.title = 'Showing ' + activeCity + ' lectures — click to change';
    } else {
      label.textContent = 'All cities';
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

    setTimeout(function () {
      document.addEventListener('click', function close(e) {
        if (!picker.contains(e.target) && e.target !== pill) {
          picker.remove();
          document.removeEventListener('click', close);
        }
      });
    }, 0);
  }

  // ---- Filter logic (topic + city) -------------------------------------

  function applyFilter() {
    const items = document.querySelectorAll('.lecture-item');
    let visibleCount = 0;

    items.forEach(function (item) {
      let show = true;

      if (activeTopic) {
        const tags = (item.dataset.tags || '').split(',').filter(Boolean);
        show = tags.includes(activeTopic);
      }

      if (show && activeCity) {
        const id = item.dataset.id;
        const lecture = lectures.find(function (l) { return l.id === id; });
        const itemCity = lecture ? extractCity(lecture.location || '') : null;
        show = itemCity === activeCity;
      }

      item.hidden = !show;
      if (show) visibleCount++;
    });

    document.querySelectorAll('.date-group').forEach(function (group) {
      group.hidden = group.querySelectorAll('.lecture-item:not([hidden])').length === 0;
    });

    const noResults = document.getElementById('no-results');
    if (noResults) {
      noResults.hidden = visibleCount > 0 || (!activeTopic && !activeCity);
    }

    document.querySelectorAll('.lecture-grid').forEach(function (grid) {
      const visible = Array.from(grid.querySelectorAll('.lecture-item:not([hidden])'));

      // Reset all overrides first
      visible.forEach(function (item) { item.style.gridColumn = ''; });

      if (window.innerWidth < 640) return;

      const n = visible.length;

      if (n === 1) {
        visible[0].style.gridColumn = '1 / -1';
      } else if (n === 2) {
        visible[0].style.gridColumn = '1 / span 2';
        visible[1].style.gridColumn = '3';
      } else if (n === 4) {
        visible[3].style.gridColumn = '1 / -1';
      } else if (n === 5) {
        visible[3].style.gridColumn = '1 / span 2';
        visible[4].style.gridColumn = '3';
      }
      // n === 3 or n === 6: no overrides needed
    });

  }

  // ---- URL topic param sync --------------------------------------------

  function getTopicFromURL() {
    try { return new URLSearchParams(window.location.search).get('topic') || ''; }
    catch (e) { return ''; }
  }

  function setTopicInURL(slug) {
    try {
      const url = new URL(window.location.href);
      if (slug) { url.searchParams.set('topic', slug); } else { url.searchParams.delete('topic'); }
      window.history.replaceState(null, '', url.toString());
    } catch (e) { /* ignore */ }
  }

  // ---- Public API ------------------------------------------------------

  window.clearFilter = function () {
    setTopic(null);
  };

  // ---- City pill wiring ------------------------------------------------

  const cityPill = document.getElementById('city-pill');
  if (cityPill) {
    cityPill.addEventListener('click', showCityPicker);
  }

  // ---- Init ------------------------------------------------------------

  renderCityPill();

  const initialTopic = getTopicFromURL();
  if (initialTopic) {
    activeTopic = initialTopic;
    const chip = document.querySelector('.topic-chip[data-topic="' + initialTopic + '"]');
    if (chip) chip.classList.add('topic-chip--active');
  }

  applyFilter();

})();
