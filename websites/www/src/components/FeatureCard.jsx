import { Fragment } from 'react';
import { Icon } from './icons.jsx';
import { Badge } from './badges.jsx';

function Glow() {
  return (
    <div className="absolute -inset-px flex justify-center items-center rounded-lg overflow-hidden opacity-0 transition-opacity duration-[.5s] group-hover:opacity-100">
      <div className="absolute inset-0 flex justify-center items-center pointer-events-none" style={{ mask: 'radial-gradient(500px circle at 0px 0px, white 0%, transparent 100%)' }}>
        <div className="flex items-center justify-center min-w-[250%] min-h-[250%]">
          <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 770 700" className="opacity-50 dark:opacity-30 animate-rotate-blob" preserveAspectRatio="xMidYMid slice">
            <g filter="url(#filter0_f_293_11490)">
              <path fill="url(#paint0_linear_293_11490)" fillOpacity="0.25" d="M409.868 129.745c-60.699-3.894-31.436 128.458-226.806 202.618-157.42 59.755 74.367 271.633 155.071 233.99 80.704-37.644 318.739-183.758 301.761-267.143-17.922-88.019-201.41-167.629-230.026-169.465z" />
            </g>
            <defs>
              <filter id="filter0_f_293_11490" width="769.26" height="699.142" x="0.497" y="0.661" colorInterpolationFilters="sRGB" filterUnits="userSpaceOnUse">
                <feFlood floodOpacity="0" result="BackgroundImageFix" />
                <feBlend in="SourceGraphic" in2="BackgroundImageFix" result="shape" />
                <feGaussianBlur result="effect1_foregroundBlur_293_11490" stdDeviation="64.5" />
              </filter>
              <linearGradient id="paint0_linear_293_11490" x1="170.1" x2="414.384" y1="411.597" y2="37.441" gradientUnits="userSpaceOnUse">
                <stop stopColor="#FF3194" />
                <stop offset="0.432" stopColor="#5024CC" />
                <stop offset="0.913" stopColor="#31FFF3" />
              </linearGradient>
            </defs>
          </svg>
        </div>
      </div>
    </div>
  );
}

function BadgeIcon({ badge }) {
  if (badge.kind === 'custom') return <Badge name={badge.name} />;
  return <Icon name={badge.name} />;
}

export function FeatureCard({ icon, hue, title, desc, bullets }) {
  const badge = typeof icon === 'string' ? { kind: 'feather', name: icon } : icon;
  return (
    <div className="group relative w-full p-6 md:p-8 bg-white dark:bg-white dark:bg-opacity-[1.5%] border border-gray-200 dark:border-white dark:border-opacity-[8%] rounded-lg">
      <Glow />
      <div size="40" className="text-current icon-container icon-40 text-2xl border border-black/10 dark:border-white/10 p-[10px] rounded-lg" style={{ backgroundColor: `hsla(${hue}, 65%, 46%, 0.25)`, color: 'white' }} aria-hidden="true">
        <BadgeIcon badge={badge} />
      </div>
      <p className="text-h5 font-semibold mt-4 mb-2 text-gray-900 dark:text-white">{title}</p>
      <p className="text-base text-gray-600 dark:text-white dark:opacity-75">{desc}</p>
      {bullets?.length > 0 && (
        <ul className="flex flex-col gap-3 mt-8">
          {bullets.map((b, i) => (
            <Fragment key={i}>
              <li className="grid gap-3 items-center grid-cols-[auto_1fr] text-gray-600 dark:text-white dark:opacity-50">
                <div className="text-current icon-container icon-md text-2xl ml-3" aria-hidden="true">
                  <BadgeIcon badge={b.kind ? b : { kind: 'feather', name: b.icon }} />
                </div>
                <p className="text-sm">{b.text}</p>
              </li>
              {i < bullets.length - 1 && (
                <div aria-hidden="true" className="ml-10 h-px bg-gradient-to-r from-black/10 dark:from-white/10 to-transparent" />
              )}
            </Fragment>
          ))}
        </ul>
      )}
    </div>
  );
}
