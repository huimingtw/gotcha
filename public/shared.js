const API = '';

function toast(msg, duration = 2500) {
  const el = document.getElementById('toast');
  if (!el) return;
  el.textContent = msg;
  el.classList.add('show');
  setTimeout(() => el.classList.remove('show'), duration);
}

function getMyName() { return localStorage.getItem('gotcha_name') || ''; }
function setMyName(name) { localStorage.setItem('gotcha_name', name); }
function clearMyName() { localStorage.removeItem('gotcha_name'); }

function confetti() {
  const colors = ['#FFD700','#E53935','#43A047','#fff','#FF9800'];
  for (let i = 0; i < 60; i++) {
    const el = document.createElement('div');
    el.className = 'confetti-piece';
    el.style.left = Math.random() * 100 + 'vw';
    el.style.top = '-20px';
    el.style.background = colors[Math.floor(Math.random() * colors.length)];
    el.style.animationDelay = Math.random() * 0.6 + 's';
    el.style.animationDuration = (0.8 + Math.random() * 0.6) + 's';
    document.body.appendChild(el);
    setTimeout(() => el.remove(), 2000);
  }
}

function sideConfetti() {
  const colors = ['#FFD700','#E53935','#43A047','#FF9800','#9C27B0','#2196F3','#FF6B9D','#fff'];
  const W = window.innerWidth;
  const H = window.innerHeight;

  function burst(fromLeft) {
    for (let i = 0; i < 28; i++) {
      const el = document.createElement('div');
      el.className = 'confetti-side-piece';
      const size = 6 + Math.random() * 8;
      const xDir = fromLeft ? 1 : -1;

      // Start near bottom corner, slightly spread
      const startX = fromLeft ? Math.random() * 50 : W - Math.random() * 50;
      const startY = H - Math.random() * 30;

      el.style.cssText = `position:fixed;z-index:9999;pointer-events:none;`
        + `border-radius:${Math.random() > 0.5 ? '50%' : '3px'};`
        + `width:${size}px;height:${size}px;`
        + `background:${colors[Math.floor(Math.random() * colors.length)]};`
        + `left:${startX}px;top:${startY}px;`;

      document.body.appendChild(el);

      // Parabolic arc: shoot up-and-inward, peak, then fall with gravity
      const xTravel = W * (0.2 + Math.random() * 0.35);   // total horizontal travel
      const yUp     = H * (0.35 + Math.random() * 0.35);   // peak height above start
      const yDrop   = H * (0.25 + Math.random() * 0.35);   // fall below start after peak
      const peak    = 0.38 + Math.random() * 0.14;          // fraction of duration at peak
      const dur     = 1400 + Math.random() * 900;
      const rot     = (Math.random() * 600 + 200) * (fromLeft ? 1 : -1);

      el.animate([
        { offset: 0,    transform: `translate(0px,0px) rotate(0deg)`,                          opacity: 1 },
        { offset: peak, transform: `translate(${xDir * xTravel * peak}px,${-yUp}px) rotate(${rot * peak}deg)`, opacity: 1 },
        { offset: 1,    transform: `translate(${xDir * xTravel}px,${yDrop}px) rotate(${rot}deg)`,              opacity: 0 },
      ], { duration: dur, easing: 'linear', fill: 'forwards' });

      setTimeout(() => el.remove(), dur + 100);
    }
  }

  burst(true);
  burst(false);
}

let eventSource = null;
function connectSSE(handlers) {
  if (eventSource) eventSource.close();
  eventSource = new EventSource('/events');
  eventSource.onmessage = (e) => {
    try {
      const msg = JSON.parse(e.data);
      if (handlers[msg.type]) handlers[msg.type](msg);
    } catch {}
  };
  eventSource.onerror = () => {
    eventSource.close();
    eventSource = null;
    setTimeout(() => connectSSE(handlers), 3000);
  };
}
