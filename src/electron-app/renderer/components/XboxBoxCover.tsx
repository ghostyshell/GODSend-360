import { useRef, useState, useEffect } from "react";

/**
 * Realistic Xbox 360 game box rendered with CSS 3D transforms.
 *
 * Two input modes:
 *   • `bookletSrc` — single image with the full back-spine-front layout
 *     (decoded RXEA cover). Each face crops its own UV region via CSS
 *     background-size / background-position.
 *   • `frontSrc`   — flat front-only cover (XboxUnity / CDN image). The
 *     back and spine fall back to a generic dark Xbox 360 case face.
 *
 * Uses pure CSS 3D transforms instead of WebGL to avoid browser limits on
 * concurrent WebGL contexts (~16 in Chromium). This allows hundreds of
 * covers to render simultaneously in a grid without context eviction.
 *
 * `flipped` is controlled by the parent so hover/focus state lives in the
 * surrounding component.
 */

interface XboxBoxCoverProps {
  bookletSrc?: string | null;
  frontSrc?: string | null;
  width?: string | number;
  height?: string | number;
  greyed?: boolean;
  flipped?: boolean;
}

export default function XboxBoxCover({
  bookletSrc,
  frontSrc,
  width = "100%",
  height = "100%",
  greyed = false,
  flipped = false,
}: XboxBoxCoverProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [cw, setCw] = useState(0);

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const ro = new ResizeObserver(([e]) => setCw(e.contentRect.width));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  // Xbox 360 case depth as 12% of width (0.18 / 1.5 from real case proportions)
  const depth = Math.round(cw * 0.12);
  const hd = depth / 2;

  const hasBooklet = !!bookletSrc;
  const hasFront = !!frontSrc;

  return (
    <div
      ref={containerRef}
      style={{
        width,
        height,
        perspective: "600px",
        perspectiveOrigin: "50% 40%",
        cursor: "default",
        filter: greyed ? "grayscale(1) brightness(0.6)" : "none",
      }}
    >
      {cw > 0 && (
        <div
          style={{
            width: "100%",
            height: "100%",
            position: "relative",
            transformStyle: "preserve-3d",
            transform: `translateY(${flipped ? -3 : 0}px) rotateY(${flipped ? 180 : 0}deg)`,
            transition: "transform 0.35s cubic-bezier(0.4, 0, 0.2, 1)",
          }}
        >
          {/* ───── Front face (+Z) ───── */}
          <div
            style={{
              ...FACE,
              inset: 0,
              transform: `translateZ(${hd}px)`,
              borderRadius: "3px",
              overflow: "hidden",
              ...(hasBooklet
                ? bgCrop(bookletSrc!, "217.39% 100%", "100% 0")
                : hasFront
                ? bgCover(frontSrc!)
                : { backgroundColor: DARK }),
            }}
          />

          {/* ───── Back face (-Z) ───── */}
          <div
            style={{
              ...FACE,
              inset: 0,
              transform: `rotateY(180deg) translateZ(${hd}px)`,
              borderRadius: "3px",
              overflow: "hidden",
            }}
          >
            {/* Inner div with scaleX(-1) un-mirrors the booklet back region
                that would otherwise appear flipped due to the parent's
                rotateY(180deg). */}
            <div
              style={{
                width: "100%",
                height: "100%",
                transform: "scaleX(-1)",
                ...(hasBooklet
                  ? bgCrop(bookletSrc!, "217.39% 100%", "0% 0")
                  : { backgroundColor: DARK }),
              }}
            />
          </div>

          {/* ───── Left face / spine (-X) ───── */}
          <div
            style={{
              ...FACE,
              top: 0,
              left: 0,
              width: `${depth}px`,
              height: "100%",
              transformOrigin: "left center",
              transform: `translateZ(${-hd}px) rotateY(-90deg)`,
              ...(hasBooklet
                ? bgCrop(bookletSrc!, "1250% 100%", "50% 0")
                : { backgroundColor: DARK }),
            }}
          >
            {/* Subtle shadow overlay simulating light from front-right */}
            <div
              style={{
                ...FILL,
                background:
                  "linear-gradient(to right, rgba(0,0,0,0.35), rgba(0,0,0,0.12))",
              }}
            />
          </div>

          {/* ───── Right face (+X) ───── */}
          <div
            style={{
              ...FACE,
              top: 0,
              right: 0,
              width: `${depth}px`,
              height: "100%",
              transformOrigin: "right center",
              transform: `translateZ(${-hd}px) rotateY(90deg)`,
              backgroundColor: DARK,
            }}
          >
            <div
              style={{
                ...FILL,
                background:
                  "linear-gradient(to left, rgba(0,0,0,0.3), rgba(0,0,0,0.1))",
              }}
            />
          </div>

          {/* ───── Top face (+Y) ───── */}
          <div
            style={{
              ...FACE,
              top: 0,
              left: 0,
              width: "100%",
              height: `${depth}px`,
              transformOrigin: "center top",
              transform: `translateZ(${-hd}px) rotateX(90deg)`,
              backgroundColor: DARK,
            }}
          />

          {/* ───── Bottom face (-Y) ───── */}
          <div
            style={{
              ...FACE,
              bottom: 0,
              left: 0,
              width: "100%",
              height: `${depth}px`,
              transformOrigin: "center bottom",
              transform: `translateZ(${-hd}px) rotateX(-90deg)`,
              backgroundColor: DARK,
            }}
          />
        </div>
      )}
    </div>
  );
}

/* ── Shared style constants ── */

const DARK = "#0a0d12";

const FACE: React.CSSProperties = {
  position: "absolute",
  backfaceVisibility: "hidden",
};

const FILL: React.CSSProperties = {
  position: "absolute",
  inset: 0,
  pointerEvents: "none",
};

/* ── Background helpers ── */

/** Crop a region from a booklet image using CSS background sizing. */
function bgCrop(src: string, size: string, position: string): React.CSSProperties {
  return {
    backgroundImage: `url(${src})`,
    backgroundSize: size,
    backgroundPosition: position,
    backgroundRepeat: "no-repeat",
  };
}

/** Cover-fill with a single front image. */
function bgCover(src: string): React.CSSProperties {
  return {
    backgroundImage: `url(${src})`,
    backgroundSize: "cover",
    backgroundPosition: "center",
    backgroundRepeat: "no-repeat",
  };
}
