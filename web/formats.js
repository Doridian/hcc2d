// Payload formats shared with ordinary QR code generators and readers
// (the de facto conventions popularized by ZXing): Wi-Fi, vCard, MECARD,
// mailto/MATMSG, SMSTO, tel, geo and iCalendar events.

export const TYPES = [
  { id: 'url', label: 'URL', fields: [
    { k: 'url', label: 'Website', type: 'url', placeholder: 'https://example.com', value: 'https://foxden.network' },
  ] },
  { id: 'text', label: 'Text', fields: [
    { k: 'text', label: 'Text', type: 'textarea', placeholder: 'Anything you like…' },
  ] },
  { id: 'wifi', label: 'Wi‑Fi', fields: [
    { k: 'ssid', label: 'Network name (SSID)', placeholder: 'MyNetwork' },
    { k: 'security', label: 'Security', type: 'select', options: [['WPA', 'WPA / WPA2 / WPA3'], ['WEP', 'WEP'], ['nopass', 'None (open)']] },
    { k: 'password', label: 'Password', type: 'password' },
    { k: 'hidden', label: 'Hidden network', type: 'checkbox' },
  ] },
  { id: 'vcard', label: 'Contact', fields: [
    { k: 'first', label: 'First name', half: true },
    { k: 'last', label: 'Last name', half: true },
    { k: 'org', label: 'Organization', half: true },
    { k: 'title', label: 'Job title', half: true },
    { k: 'phone', label: 'Phone', type: 'tel', half: true },
    { k: 'email', label: 'Email', type: 'email', half: true },
    { k: 'url', label: 'Website', type: 'url' },
    { k: 'street', label: 'Street' },
    { k: 'city', label: 'City', half: true },
    { k: 'zip', label: 'Postal code', half: true },
    { k: 'country', label: 'Country' },
    { k: 'note', label: 'Note', type: 'textarea' },
  ] },
  { id: 'email', label: 'Email', fields: [
    { k: 'to', label: 'To', type: 'email', placeholder: 'someone@example.com' },
    { k: 'subject', label: 'Subject' },
    { k: 'body', label: 'Message', type: 'textarea' },
  ] },
  { id: 'sms', label: 'SMS', fields: [
    { k: 'phone', label: 'Phone number', type: 'tel', placeholder: '+1 555 0100' },
    { k: 'message', label: 'Message', type: 'textarea' },
  ] },
  { id: 'tel', label: 'Phone', fields: [
    { k: 'phone', label: 'Phone number', type: 'tel', placeholder: '+1 555 0100' },
  ] },
  { id: 'geo', label: 'Location', fields: [
    { k: 'lat', label: 'Latitude', type: 'number', step: 'any', placeholder: '52.5163', half: true },
    { k: 'lng', label: 'Longitude', type: 'number', step: 'any', placeholder: '13.3777', half: true },
    { k: 'q', label: 'Label (optional)' },
  ] },
  { id: 'event', label: 'Event', fields: [
    { k: 'summary', label: 'Title' },
    { k: 'start', label: 'Starts', type: 'datetime-local', half: true },
    { k: 'end', label: 'Ends', type: 'datetime-local', half: true },
    { k: 'location', label: 'Location' },
    { k: 'description', label: 'Description', type: 'textarea' },
  ] },
];

// ── building ────────────────────────────────────────────────────────────────

const escWifi = (s) => s.replace(/([\\;,:"])/g, '\\$1');
const escVcard = (s) => s.replace(/\\/g, '\\\\').replace(/\n/g, '\\n').replace(/([,;])/g, '\\$1');
const icalDate = (s) => s.replace(/[-:]/g, '').replace(/^(\d{8}T\d{4})$/, '$100');

function lines(list) {
  return list.filter(([, v]) => v !== undefined && v !== '').map(([k, v]) => `${k}:${v}`);
}

export function build(type, v) {
  const t = (k) => (v[k] ?? '').toString().trim();
  switch (type) {
    case 'text':
      return v.text ?? '';
    case 'url': {
      const u = t('url');
      return u && !/^[a-z][a-z0-9+.-]*:/i.test(u) ? `https://${u}` : u;
    }
    case 'wifi': {
      const sec = t('security') || 'WPA';
      let s = `WIFI:T:${sec};S:${escWifi(t('ssid'))};`;
      if (sec !== 'nopass') s += `P:${escWifi(v.password ?? '')};`;
      if (v.hidden) s += 'H:true;';
      return s + ';';
    }
    case 'vcard': {
      const name = [t('first'), t('last')].filter(Boolean).join(' ');
      const adr = [t('street'), t('city'), t('zip'), t('country')].some(Boolean)
        ? `;;${escVcard(t('street'))};${escVcard(t('city'))};;${escVcard(t('zip'))};${escVcard(t('country'))}`
        : '';
      return ['BEGIN:VCARD', 'VERSION:3.0', ...lines([
        ['N', name ? `${escVcard(t('last'))};${escVcard(t('first'))};;;` : ''],
        ['FN', escVcard(name)],
        ['ORG', escVcard(t('org'))],
        ['TITLE', escVcard(t('title'))],
        ['TEL;TYPE=CELL', t('phone')],
        ['EMAIL', t('email')],
        ['URL', t('url')],
        ['ADR;TYPE=WORK', adr],
        ['NOTE', escVcard(t('note'))],
      ]), 'END:VCARD'].join('\n');
    }
    case 'email': {
      const q = new URLSearchParams();
      if (t('subject')) q.set('subject', t('subject'));
      if (t('body')) q.set('body', v.body);
      const qs = q.toString().replace(/\+/g, '%20');
      return `mailto:${t('to')}${qs ? `?${qs}` : ''}`;
    }
    case 'sms':
      return `SMSTO:${t('phone')}:${v.message ?? ''}`;
    case 'tel':
      return `tel:${t('phone').replace(/[^\d+#*]/g, '')}`;
    case 'geo': {
      const q = t('q');
      return `geo:${t('lat') || 0},${t('lng') || 0}${q ? `?q=${encodeURIComponent(q)}` : ''}`;
    }
    case 'event':
      return ['BEGIN:VEVENT', ...lines([
        ['SUMMARY', escVcard(t('summary'))],
        ['DTSTART', t('start') && icalDate(t('start'))],
        ['DTEND', t('end') && icalDate(t('end'))],
        ['LOCATION', escVcard(t('location'))],
        ['DESCRIPTION', escVcard(t('description'))],
      ]), 'END:VEVENT'].join('\n');
  }
  return '';
}

// ── parsing ─────────────────────────────────────────────────────────────────

// splitEscaped splits s at unescaped sep characters and unescapes the parts.
function splitEscaped(s, sep) {
  const out = [];
  let cur = '';
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    if (c === '\\' && i + 1 < s.length) {
      cur += s[++i];
    } else if (c === sep) {
      out.push(cur);
      cur = '';
    } else {
      cur += c;
    }
  }
  out.push(cur);
  return out;
}

// keyValues parses "K:v;K:v;;" bodies (Wi-Fi, MECARD, MATMSG).
function keyValues(body) {
  const map = {};
  for (const part of splitEscaped(body, ';')) {
    const i = part.indexOf(':');
    if (i > 0) (map[part.slice(0, i).toUpperCase()] ??= []).push(part.slice(i + 1));
  }
  return map;
}

function unescVcard(s) {
  return s.replace(/\\([\\,;nN])/g, (_, c) => (c === 'n' || c === 'N' ? '\n' : c));
}

// vLines parses vCard / iCalendar content lines into [name, params, value].
function vLines(text) {
  const unfolded = text.replace(/\r\n/g, '\n').replace(/\n[ \t]/g, '');
  return unfolded.split('\n').map((l) => {
    const i = l.indexOf(':');
    if (i < 0) return null;
    const [name, ...params] = l.slice(0, i).split(';');
    return [name.toUpperCase(), params.join(';').toUpperCase(), l.slice(i + 1)];
  }).filter(Boolean);
}

function icalToLocal(v) {
  const m = /^(\d{4})(\d{2})(\d{2})(?:T(\d{2})(\d{2})(\d{2})?(Z)?)?$/.exec(v.trim());
  if (!m) return v;
  const [, y, mo, d, h, mi, s = '00', z] = m;
  if (h === undefined) return new Date(+y, mo - 1, +d).toLocaleDateString();
  const date = z ? new Date(Date.UTC(+y, mo - 1, +d, +h, +mi, +s)) : new Date(+y, mo - 1, +d, +h, +mi, +s);
  return date.toLocaleString();
}

const safeHttp = (u) => /^https?:\/\//i.test(u);

function safeDecode(s) {
  try {
    return decodeURIComponent(s);
  } catch {
    return s;
  }
}

// parse recognizes the payload format of decoded text and returns
// {type, title, fields: [[label, value]], action?: {label, href, download?}}.
export function parse(text) {
  const trimmed = text.trim();
  const upper = trimmed.toUpperCase();

  if (upper.startsWith('WIFI:')) {
    const kv = keyValues(trimmed.slice(5));
    const sec = kv.T?.[0] || 'nopass';
    return {
      type: 'wifi', title: 'Wi‑Fi network',
      fields: [
        ['Network', kv.S?.[0] ?? ''],
        ['Security', { nopass: 'None (open)', WPA: 'WPA / WPA2 / WPA3', WEP: 'WEP' }[sec] ?? sec],
        ...(kv.P ? [['Password', kv.P[0]]] : []),
        ...(kv.H?.[0]?.toLowerCase() === 'true' ? [['Hidden', 'yes']] : []),
      ],
      copy: kv.P?.[0],
    };
  }

  if (upper.startsWith('BEGIN:VCARD')) {
    const f = [];
    let name = '';
    for (const [k, params, raw] of vLines(trimmed)) {
      const v = unescVcard(raw);
      const kind = params.match(/TYPE=([A-Z,]+)/)?.[1]?.toLowerCase();
      switch (k) {
        case 'FN': name = v; break;
        case 'N': if (!name) name = raw.split(';').slice(0, 2).reverse().map(unescVcard).join(' ').trim(); break;
        case 'ORG': f.push(['Organization', v.replace(/;/g, ', ')]); break;
        case 'TITLE': f.push(['Title', v]); break;
        case 'TEL': f.push([kind ? `Phone (${kind})` : 'Phone', v]); break;
        case 'EMAIL': f.push(['Email', v]); break;
        case 'URL': f.push(['Website', v]); break;
        case 'ADR': f.push(['Address', raw.split(';').map(unescVcard).filter(Boolean).join(', ')]); break;
        case 'NOTE': f.push(['Note', v]); break;
        case 'BDAY': f.push(['Birthday', v]); break;
      }
    }
    if (name) f.unshift(['Name', name]);
    return {
      type: 'vcard', title: 'Contact', fields: f,
      action: { label: 'Save contact (.vcf)', href: blobURL(trimmed, 'text/vcard'), download: `${name || 'contact'}.vcf` },
    };
  }

  if (upper.startsWith('MECARD:')) {
    const kv = keyValues(trimmed.slice(7));
    const f = [];
    if (kv.N) f.push(['Name', kv.N[0].split(',').reverse().join(' ').trim()]);
    for (const [k, label] of [['ORG', 'Organization'], ['TEL', 'Phone'], ['EMAIL', 'Email'], ['URL', 'Website'], ['ADR', 'Address'], ['NOTE', 'Note'], ['BDAY', 'Birthday']]) {
      for (const v of kv[k] ?? []) f.push([label, v]);
    }
    return { type: 'vcard', title: 'Contact', fields: f };
  }

  if (upper.startsWith('MAILTO:')) {
    const [addr, qs = ''] = trimmed.slice(7).split('?');
    const q = new URLSearchParams(qs);
    return {
      type: 'email', title: 'Email',
      fields: [['To', safeDecode(addr)], ...(q.get('subject') ? [['Subject', q.get('subject')]] : []), ...(q.get('body') ? [['Message', q.get('body')]] : [])],
      action: { label: 'Write email', href: trimmed },
    };
  }

  if (upper.startsWith('MATMSG:')) {
    const kv = keyValues(trimmed.slice(7));
    const to = kv.TO?.[0] ?? '';
    const q = new URLSearchParams();
    if (kv.SUB) q.set('subject', kv.SUB[0]);
    if (kv.BODY) q.set('body', kv.BODY[0]);
    return {
      type: 'email', title: 'Email',
      fields: [['To', to], ...(kv.SUB ? [['Subject', kv.SUB[0]]] : []), ...(kv.BODY ? [['Message', kv.BODY[0]]] : [])],
      action: { label: 'Write email', href: `mailto:${to}?${q.toString().replace(/\+/g, '%20')}` },
    };
  }

  if (upper.startsWith('SMSTO:') || upper.startsWith('SMS:')) {
    const rest = trimmed.slice(trimmed.indexOf(':') + 1);
    let [num, ...msg] = rest.split(':');
    let message = msg.join(':');
    if (num.includes('?')) { // sms:+123?body=…
      const [n, qs] = num.split('?');
      num = n;
      message = new URLSearchParams(qs).get('body') ?? message;
    }
    return {
      type: 'sms', title: 'Text message',
      fields: [['To', num], ...(message ? [['Message', message]] : [])],
      action: { label: 'Send SMS', href: `sms:${num}${message ? `?body=${encodeURIComponent(message)}` : ''}` },
    };
  }

  if (upper.startsWith('TEL:')) {
    const num = trimmed.slice(4);
    return { type: 'tel', title: 'Phone number', fields: [['Number', num]], action: { label: 'Call', href: `tel:${num}` } };
  }

  if (upper.startsWith('GEO:')) {
    const [coords, qs = ''] = trimmed.slice(4).split('?');
    const [lat, lng, alt] = coords.split(',').map((s) => s.trim());
    const label = new URLSearchParams(qs).get('q');
    const f = [['Latitude', lat], ['Longitude', lng]];
    if (alt) f.push(['Altitude', `${alt} m`]);
    if (label) f.push(['Label', label]);
    const ok = isFinite(+lat) && isFinite(+lng);
    return {
      type: 'geo', title: 'Location', fields: f,
      action: ok ? { label: 'Show on OpenStreetMap', href: `https://www.openstreetmap.org/?mlat=${+lat}&mlon=${+lng}#map=16/${+lat}/${+lng}` } : undefined,
    };
  }

  if (upper.startsWith('BEGIN:VEVENT') || upper.startsWith('BEGIN:VCALENDAR')) {
    const f = [];
    let title = 'Event';
    for (const [k, , raw] of vLines(trimmed)) {
      const v = unescVcard(raw);
      switch (k) {
        case 'SUMMARY': title = v; break;
        case 'DTSTART': f.push(['Starts', icalToLocal(v)]); break;
        case 'DTEND': f.push(['Ends', icalToLocal(v)]); break;
        case 'LOCATION': f.push(['Location', v]); break;
        case 'DESCRIPTION': f.push(['Description', v]); break;
      }
    }
    const ics = upper.startsWith('BEGIN:VCALENDAR') ? trimmed
      : `BEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//hcc2d//demo//EN\n${trimmed}\nEND:VCALENDAR`;
    return {
      type: 'event', title, fields: f,
      action: { label: 'Add to calendar (.ics)', href: blobURL(ics, 'text/calendar'), download: 'event.ics' },
    };
  }

  if (/^[a-z][a-z0-9+.-]*:\/\/\S+$/i.test(trimmed)) {
    return {
      type: 'url', title: 'Link', fields: [['URL', trimmed]],
      action: safeHttp(trimmed) ? { label: 'Open link', href: trimmed } : undefined,
    };
  }

  return { type: 'text', title: 'Text', fields: [] };
}

function blobURL(text, type) {
  return URL.createObjectURL(new Blob([text], { type }));
}
