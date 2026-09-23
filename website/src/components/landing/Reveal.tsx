import {useEffect, useRef, useState, type CSSProperties, type ReactNode} from 'react';
import clsx from 'clsx';

type Props = {
  children: ReactNode;
  className?: string;
  style?: CSSProperties;
  delay?: number;
};

// Adds `is-visible` once the element scrolls into view. The matching
// styles, and the fallback for readers without JavaScript, are global in
// custom.css and docusaurus.config.ts.
export default function Reveal({children, className, style, delay = 0}: Readonly<Props>) {
  const ref = useRef<HTMLDivElement>(null);
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    const node = ref.current;
    if (!node || typeof IntersectionObserver === 'undefined') {
      setVisible(true);
      return undefined;
    }
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          setVisible(true);
          observer.disconnect();
        }
      },
      {rootMargin: '0px 0px -8% 0px'},
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, []);

  return (
    <div
      ref={ref}
      className={clsx('reveal', visible && 'is-visible', className)}
      style={delay ? {...style, transitionDelay: `${delay}ms`} : style}>
      {children}
    </div>
  );
}
