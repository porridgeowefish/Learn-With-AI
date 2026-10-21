// One inline SVG icon set for hover action affordances across the app.
// Stroke/fill inherit currentColor so icons follow text color; sizes are
// uniform at 16px with 1.6 stroke width for a quiet, consistent look.
import type { SVGProps } from 'react';

type IconProps = SVGProps<SVGSVGElement> & { size?: number };

function svgProps({ size = 16, ...rest }: IconProps) {
  return {
    width: size,
    height: size,
    viewBox: '0 0 24 24',
    fill: 'none',
    stroke: 'currentColor',
    strokeWidth: 1.6,
    strokeLinecap: 'round' as const,
    strokeLinejoin: 'round' as const,
    'aria-hidden': true,
    ...rest,
  };
}

export function CopyIcon(props: IconProps) {
  return (<svg {...svgProps(props)}><rect x="9" y="9" width="11" height="11" rx="2" /><path d="M5 15V6a2 2 0 0 1 2-2h9" /></svg>);
}

export function RegenerateIcon(props: IconProps) {
  return (<svg {...svgProps(props)}><path d="M20 11a8 8 0 1 0-2.3 6.3" /><path d="M20 5v6h-6" /></svg>);
}

export function SteerIcon(props: IconProps) {
  return (<svg {...svgProps(props)}><path d="M13 2 4.5 13h5L9 22l8.5-11h-5L13 2Z" /></svg>);
}

export function EditIcon(props: IconProps) {
  return (<svg {...svgProps(props)}><path d="M4 20h4L19 9a2.1 2.1 0 0 0-3-3L5 17v3Z" /><path d="m13.5 6.5 3 3" /></svg>);
}

export function TrashIcon(props: IconProps) {
  return (<svg {...svgProps(props)}><path d="M4 7h16" /><path d="M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2" /><path d="M6 7l1 13h10l1-13" /></svg>);
}

export function DownloadIcon(props: IconProps) {
  return (<svg {...svgProps(props)}><path d="M12 3v12" /><path d="m7 11 5 5 5-5" /><path d="M4 20h16" /></svg>);
}

export function SearchIcon(props: IconProps) {
  return (<svg {...svgProps(props)}><circle cx="11" cy="11" r="6.5" /><path d="m16 16 4.5 4.5" /></svg>);
}

export function UploadIcon(props: IconProps) {
  return <svg {...svgProps(props)}><path d="M12 5v14M5 12h14" /></svg>;
}
export function MicrophoneIcon(props: IconProps) {
  return <svg {...svgProps(props)}><rect x="9" y="2" width="6" height="12" rx="3" /><path d="M5 10v2a7 7 0 0 0 14 0v-2M12 19v3M8 22h8" /></svg>;
}
export function SendIcon(props: IconProps) {
  return <svg {...svgProps(props)}><path d="M12 20V4m-7 7 7-7 7 7" /></svg>;
}
export function StopIcon(props: IconProps) {
  return <svg {...svgProps(props)}><rect x="6" y="6" width="12" height="12" rx="2" fill="currentColor" stroke="none" /></svg>;
}
