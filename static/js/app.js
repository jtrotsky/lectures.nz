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
    if (slug) track('topic_filter', { topic: slug });
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

  function lectureCity(lecture) {
    const locCity = extractCity(lecture.location || '');
    if (locCity) return locCity;
    const hostMap = window.HOST_CITY || {};
    return hostMap[lecture.host_slug] || null;
  }

  function updateRSSLink(city) {
    const link = document.getElementById('rss-link');
    if (!link) return;
    if (city) {
      link.href = '/feed/' + city.toLowerCase() + '.xml';
      link.textContent = city + ' RSS Feed';
    } else {
      link.href = '/rss.xml';
      link.textContent = 'RSS Feed';
    }
  }

  function setCity(city) {
    activeCity = city || null;
    if (activeCity) {
      localStorage.setItem(CITY_KEY, activeCity);
    } else {
      localStorage.removeItem(CITY_KEY);
    }
    track('city_filter', { city: activeCity || 'all' });
    renderCityPill();
    updateRSSLink(activeCity);
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
      const c = lectureCity(l);
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
        show = lecture ? lectureCity(lecture) === activeCity : false;
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
      if (window.innerWidth < 640) return;
      const visible = Array.from(grid.querySelectorAll('.lecture-item:not([hidden])'));

      // Reset all overrides
      visible.forEach(function (item) { item.style.gridColumn = ''; });

      if (visible.length === 1) {
        visible[0].style.gridColumn = '1 / 3';
      } else if (visible.length === 2) {
        var lengths = visible.map(function(item) {
          var title = item.querySelector('.lecture-title');
          return title ? title.textContent.trim().length : 0;
        });
        if (lengths[0] >= lengths[1]) {
          // First card has longer title: cols 1-2, second gets col 3
          visible[0].style.gridColumn = '1 / span 2';
          visible[1].style.gridColumn = '3';
        } else {
          // Second card has longer title: first gets col 1, second gets cols 2-3
          visible[0].style.gridColumn = '1';
          visible[1].style.gridColumn = '2 / span 2';
        }
      } else if (visible.length === 4) {
        visible[3].style.gridColumn = '1 / 3';
      }
      // 3, 5, 6: no overrides — empty columns are acceptable
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
    track('clear_filter');
    setTopic(null);
  };

  // ---- Lecture card click tracking -------------------------------------

  document.querySelectorAll('.lecture-item').forEach(function (card) {
    card.addEventListener('click', function () {
      track('lecture_click', { host: card.dataset.host || '', id: card.dataset.id || '' });
    });
  });

  // ---- City pill wiring ------------------------------------------------

  const cityPill = document.getElementById('city-pill');
  if (cityPill) {
    cityPill.addEventListener('click', showCityPicker);
  }

  // ---- Init ------------------------------------------------------------

  renderCityPill();
  updateRSSLink(activeCity);

  const initialTopic = getTopicFromURL();
  if (initialTopic) {
    activeTopic = initialTopic;
    const chip = document.querySelector('.topic-chip[data-topic="' + initialTopic + '"]');
    if (chip) chip.classList.add('topic-chip--active');
  }

  applyFilter();

})();
