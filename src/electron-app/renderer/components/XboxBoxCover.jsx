import { useMemo, useRef, useState, useEffect } from "react";
import { Canvas, useFrame } from "@react-three/fiber";
import * as THREE from "three";

/**
 * Realistic Xbox 360 game box rendered with three.js.
 *
 * Two input modes:
 *   • `bookletSrc` — single image with the full back-spine-front layout
 *     (decoded RXEA cover). Each face samples its own UV region.
 *   • `frontSrc`   — flat front-only cover (XboxUnity / CDN image). The
 *     back and spine fall back to a generic dark Xbox 360 case face.
 *
 * `flipped` is controlled by the parent so hover/focus state lives in the
 * surrounding component.
 */
export default function XboxBoxCover({
  bookletSrc,
  frontSrc,
  width = "100%",
  height = "100%",
  greyed = false,
  flipped = false,
}) {
  return (
    <div
      style={{
        width,
        height,
        cursor: "default",
        filter: greyed ? "grayscale(1) brightness(0.6)" : "none",
      }}
    >
      <Canvas
        dpr={[1, 2]}
        camera={{ position: [0, 0, 3.8], fov: 32 }}
        gl={{ antialias: true, alpha: true, preserveDrawingBuffer: false }}
        style={{ background: "transparent" }}
      >
        <ambientLight intensity={0.85} />
        <directionalLight position={[3, 4, 5]} intensity={0.55} />
        <directionalLight position={[-3, -2, 2]} intensity={0.25} />
        <BoxMesh
          bookletSrc={bookletSrc}
          frontSrc={frontSrc}
          flipped={flipped}
        />
      </Canvas>
    </div>
  );
}

function BoxMesh({ bookletSrc, frontSrc, flipped }) {
  const ref = useRef();
  const targetY = flipped ? Math.PI : 0;

  useFrame((_, dt) => {
    if (!ref.current) return;
    const cur = ref.current.rotation.y;
    const diff = targetY - cur;
    ref.current.rotation.y = cur + diff * Math.min(1, dt * 6);
    // subtle lift on flip
    const targetLift = flipped ? 0.08 : 0;
    ref.current.position.y += (targetLift - ref.current.position.y) * Math.min(1, dt * 6);
  });

  const materials = useBoxMaterials(bookletSrc, frontSrc);

  // Xbox 360 game case proportions — taller than wide, with a thin spine.
  // box dims: width 1.5, height 2.0, depth 0.18 (≈ 12mm-thick case).
  return (
    <mesh ref={ref} material={materials}>
      <boxGeometry args={[1.5, 2.0, 0.18]} />
    </mesh>
  );
}

/**
 * Build six face materials in three.js BoxGeometry order:
 *   [+X, -X, +Y, -Y, +Z, -Z]
 *   right, left, top, bottom, front, back
 *
 * For a booklet image (back | spine | front, laid out left to right):
 *   front  (+Z) → right ~46% of image
 *   back   (-Z) → left  ~46% of image
 *   spine  (-X) → middle ~8%, displayed on the LEFT edge of the case (matches
 *                 a real Xbox 360 case where the spine is on the left when
 *                 the front faces you)
 *
 * For a flat front cover, the front face uses the image and the other faces
 * fall back to a dark plastic colour.
 */
function useBoxMaterials(bookletSrc, frontSrc) {
  const [bookletTex, setBookletTex] = useState(null);
  const [frontTex,   setFrontTex]   = useState(null);

  useEffect(() => {
    if (!bookletSrc) { setBookletTex(null); return; }
    const loader = new THREE.TextureLoader();
    loader.setCrossOrigin("anonymous");
    loader.load(
      bookletSrc,
      (t) => {
        t.colorSpace = THREE.SRGBColorSpace;
        t.minFilter = THREE.LinearMipmapLinearFilter;
        t.anisotropy = 8;
        setBookletTex(t);
      },
      undefined,
      () => setBookletTex(null),
    );
    return () => {};
  }, [bookletSrc]);

  useEffect(() => {
    if (!frontSrc) { setFrontTex(null); return; }
    const loader = new THREE.TextureLoader();
    loader.setCrossOrigin("anonymous");
    loader.load(
      frontSrc,
      (t) => {
        t.colorSpace = THREE.SRGBColorSpace;
        t.minFilter = THREE.LinearMipmapLinearFilter;
        t.anisotropy = 8;
        setFrontTex(t);
      },
      undefined,
      () => setFrontTex(null),
    );
    return () => {};
  }, [frontSrc]);

  return useMemo(() => {
    const plastic = new THREE.MeshStandardMaterial({
      color: "#0a0d12",
      roughness: 0.7,
      metalness: 0.05,
    });

    if (bookletTex) {
      // Sample regions of the booklet image. Width fractions: back ~0.46,
      // spine ~0.08, front ~0.46. Each face material uses a cloned texture
      // with its own offset/repeat (UV crop).
      const cropX = (xStart, xWidth) => {
        const t = bookletTex.clone();
        t.needsUpdate = true;
        t.colorSpace = THREE.SRGBColorSpace;
        t.wrapS = t.wrapT = THREE.ClampToEdgeWrapping;
        t.repeat.set(xWidth, 1);
        t.offset.set(xStart, 0);
        return t;
      };

      const FRONT_W = 0.46;
      const SPINE_W = 0.08;
      const BACK_W  = 0.46;
      const backTex  = cropX(0,             BACK_W);
      const spineTex = cropX(BACK_W,        SPINE_W);
      const frontT   = cropX(BACK_W + SPINE_W, FRONT_W);

      const matFront = new THREE.MeshStandardMaterial({
        map: frontT, roughness: 0.55, metalness: 0.05,
      });
      const matBack = new THREE.MeshStandardMaterial({
        map: backTex, roughness: 0.55, metalness: 0.05,
      });
      const matSpineLeft = new THREE.MeshStandardMaterial({
        map: spineTex, roughness: 0.55, metalness: 0.05,
      });

      // [+X right, -X left/spine, +Y top, -Y bottom, +Z front, -Z back]
      return [plastic, matSpineLeft, plastic, plastic, matFront, matBack];
    }

    if (frontTex) {
      const matFront = new THREE.MeshStandardMaterial({
        map: frontTex, roughness: 0.55, metalness: 0.05,
      });
      // generic "back of Xbox 360 case" placeholder — dark plastic
      return [plastic, plastic, plastic, plastic, matFront, plastic];
    }

    return [plastic, plastic, plastic, plastic, plastic, plastic];
  }, [bookletTex, frontTex]);
}
