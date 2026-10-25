// Renders a Humagen design board: a <template data-board> holding markup with
// {{ holes }}, <sc-for> and <sc-if>, driven by a <script type="text/x-dc"> class
// that extends DCLogic. No dependencies; re-renders the board on setState.
(() => {
  class DCLogic {
    constructor(props) {
      this.props = props || {};
      this.state = {};
    }
    setState(patch) {
      const next = typeof patch === 'function' ? patch(this.state, this.props) : patch;
      this.state = { ...this.state, ...next };
      this.forceUpdate();
    }
    forceUpdate() {
      if (this.__render) this.__render();
    }
  }
  window.DCLogic = DCLogic;

  const HOLE = /\{\{\s*([^}]*?)\s*\}\}/g;
  const WHOLE = /^\s*\{\{\s*([^}]*?)\s*\}\}\s*$/;
  const BOOL_PROPS = new Set(['checked', 'disabled', 'selected', 'hidden', 'open', 'required', 'readonly']);

  function lookup(expr, scope) {
    if (expr === 'true') return true;
    if (expr === 'false') return false;
    if (expr === 'null') return null;
    if (/^-?\d+(\.\d+)?$/.test(expr)) return Number(expr);
    const q = /^(['"])(.*)\1$/.exec(expr);
    if (q) return q[2];
    let v = scope;
    for (const part of expr.split('.')) {
      if (v == null) return undefined;
      v = v[part];
    }
    return v;
  }

  function interpolate(text, scope) {
    return text.replace(HOLE, (_, e) => {
      const v = lookup(e, scope);
      return v == null ? '' : String(v);
    });
  }

  function renderNodes(nodes, scope, out) {
    for (const n of nodes) renderNode(n, scope, out);
    return out;
  }

  function renderNode(node, scope, out) {
    if (node.nodeType === Node.TEXT_NODE) {
      out.push(document.createTextNode(interpolate(node.nodeValue, scope)));
      return;
    }
    if (node.nodeType !== Node.ELEMENT_NODE) return;
    const tag = node.localName;
    if (tag === 'helmet') return;
    if (tag === 'sc-for') {
      const list = lookup((WHOLE.exec(node.getAttribute('list') || '') || [])[1] || '', scope) || [];
      const as = node.getAttribute('as') || 'item';
      Array.from(list).forEach((item, i) => {
        renderNodes(node.childNodes, { ...scope, [as]: item, $index: i }, out);
      });
      return;
    }
    if (tag === 'sc-if') {
      const v = lookup((WHOLE.exec(node.getAttribute('value') || '') || [])[1] || '', scope);
      if (v) renderNodes(node.childNodes, scope, out);
      return;
    }
    const el = document.createElementNS(node.namespaceURI, node.localName);
    for (const { name, value } of Array.from(node.attributes)) {
      if (name.startsWith('hint-')) continue;
      const whole = WHOLE.exec(value);
      if (name.startsWith('on') && whole) {
        const fn = lookup(whole[1], scope);
        if (typeof fn === 'function') el.addEventListener(name.slice(2), (ev) => fn(ev));
        continue;
      }
      if (whole) {
        const v = lookup(whole[1], scope);
        if (BOOL_PROPS.has(name)) {
          if (v) el.setAttribute(name, '');
          el[name] = !!v;
          continue;
        }
        if (v == null) continue;
        el.setAttribute(name, String(v));
        continue;
      }
      el.setAttribute(name, value.includes('{{') ? interpolate(value, scope) : value);
    }
    renderNodes(node.childNodes, scope, []).forEach((c) => el.appendChild(c));
    out.push(el);
  }

  function boot() {
    const tpl = document.querySelector('template[data-board]');
    const script = document.querySelector('script[type="text/x-dc"]');
    if (!tpl) return;
    const content = tpl.content;
    const helmet = content.querySelector('helmet');
    if (helmet) for (const c of Array.from(helmet.children)) document.head.appendChild(c.cloneNode(true));

    let defs = {};
    try {
      defs = JSON.parse((script && script.getAttribute('data-props')) || '{}');
    } catch (_) {}
    const props = {};
    for (const [k, d] of Object.entries(defs)) if (!k.startsWith('$') && d && 'default' in d) props[k] = d.default;

    const Component = script
      ? new Function('DCLogic', `${script.textContent}\nreturn Component;`)(DCLogic)
      : class extends DCLogic { renderVals() { return {}; } };
    const instance = new Component(props);
    const root = document.createElement('div');
    root.className = 'board-root';
    tpl.replaceWith(root);

    instance.__render = () => {
      const x = window.scrollX, y = window.scrollY;
      const nodes = renderNodes(content.childNodes, instance.renderVals() || {}, []);
      root.replaceChildren(...nodes);
      window.scrollTo(x, y);
    };
    instance.__render();
    if (typeof instance.componentDidMount === 'function') instance.componentDidMount();
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', boot);
  else boot();
})();
