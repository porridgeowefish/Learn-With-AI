import createDOMPurify from 'dompurify';

const purifier = createDOMPurify(window);
const rasterSource = /^data:image\/(?:png|jpe?g|gif|webp);base64,[a-z0-9+/]+={0,2}$/i;
const localReference = /^#[\w:.-]+$/;
const svgPaintAttributes = new Set(['fill', 'stroke', 'clip-path', 'mask', 'marker-start', 'marker-mid', 'marker-end']);

purifier.addHook('uponSanitizeAttribute', (node, attr) => {
  if (attr.attrName === 'src') attr.keepAttr = node.nodeName === 'IMG' && rasterSource.test(attr.attrValue);
  if (attr.attrName === 'href' || attr.attrName === 'xlink:href') attr.keepAttr = localReference.test(attr.attrValue);
  if (svgPaintAttributes.has(attr.attrName) && /url\s*\(/i.test(attr.attrValue)) {
    attr.keepAttr = /^url\(\s*#[\w:.-]+\s*\)$/i.test(attr.attrValue);
  }
});

export function sanitizeReview(content: string): DocumentFragment {
  const fragment = purifier.sanitize(content, {
    ALLOWED_TAGS: [
      'article', 'section', 'header', 'footer', 'div', 'span', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
      'p', 'strong', 'em', 'b', 'i', 'small', 'sub', 'sup', 'blockquote', 'ul', 'ol', 'li', 'dl', 'dt', 'dd',
      'table', 'caption', 'thead', 'tbody', 'tfoot', 'tr', 'th', 'td', 'pre', 'code', 'br', 'hr',
      'figure', 'figcaption', 'img', 'details', 'summary', 'label', 'meter', 'progress',
      'svg', 'title', 'desc', 'g', 'defs', 'path', 'line', 'polyline', 'polygon', 'circle', 'ellipse', 'rect',
      'text', 'tspan', 'marker', 'use', 'symbol', 'clippath', 'mask', 'lineargradient', 'radialgradient', 'stop',
      'math', 'mrow', 'mi', 'mn', 'mo', 'mtext', 'mfrac', 'msqrt', 'mroot', 'msub', 'msup', 'msubsup',
      'munder', 'mover', 'munderover', 'mtable', 'mtr', 'mtd', 'mspace', 'mstyle',
    ],
    ALLOWED_ATTR: [
      'colspan', 'rowspan', 'scope', 'start', 'lang', 'src', 'alt', 'width', 'height', 'open', 'value',
      'min', 'max', 'low', 'high', 'optimum', 'id', 'role', 'aria-label', 'aria-labelledby', 'display', 'mathvariant',
      'viewbox', 'preserveaspectratio', 'href', 'xlink:href', 'x', 'y', 'x1', 'x2', 'y1', 'y2',
      'cx', 'cy', 'r', 'rx', 'ry', 'd', 'points', 'transform', 'fill', 'fill-opacity', 'fill-rule', 'stroke',
      'stroke-width', 'stroke-opacity', 'stroke-linecap', 'stroke-linejoin', 'stroke-dasharray', 'opacity',
      'text-anchor', 'dominant-baseline', 'font-size', 'marker-start', 'marker-mid', 'marker-end', 'markerwidth',
      'markerheight', 'markerunits', 'refx', 'refy', 'orient', 'clip-path', 'mask', 'gradientunits',
      'gradienttransform', 'offset', 'stop-color', 'stop-opacity',
    ],
    FORBID_TAGS: ['foreignobject', 'annotation-xml'],
    ALLOW_ARIA_ATTR: false,
    ALLOW_DATA_ATTR: false,
    RETURN_DOM_FRAGMENT: true,
  });
  fragment.querySelectorAll('img:not([src])').forEach(image => image.remove());
  return fragment;
}
