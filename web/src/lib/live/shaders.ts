// PLAN 74: moving photos for the login page and the desktop. Each shader
// moves one of the built-in pictures (static/backgrounds/<id>.webp) a
// little: the aurora over the lake and its reflection, the water of the
// mountain lake and the clouds above it. Plain WebGL 1, no library.

/** The pictures that can move, and where their horizon lies (0 = top). */
export const LIVE_PICTURES = ['nordlicht', 'bergsee'] as const;
export type LivePicture = (typeof LIVE_PICTURES)[number];

export function isLivePicture(id: string): id is LivePicture {
	return (LIVE_PICTURES as readonly string[]).includes(id);
}

export const VERTEX = `attribute vec2 p;varying vec2 v;void main(){v=p*.5+.5;gl_Position=vec4(p,0.,1.);}`;

// The screen point in picture coordinates (as background-size: cover, y down),
// with a slow zoom and a small shift toward the pointer.
const COMMON = `precision highp float;varying vec2 v;uniform vec2 res;uniform float t;uniform vec2 m;uniform sampler2D img;uniform vec2 isz;
float h(vec2 p){return fract(sin(dot(p,vec2(127.1,311.7)))*43758.5453);}
float n(vec2 p){vec2 i=floor(p),f=fract(p);f=f*f*(3.-2.*f);return mix(mix(h(i),h(i+vec2(1,0)),f.x),mix(h(i+vec2(0,1)),h(i+vec2(1,1)),f.x),f.y);}
float fbm(vec2 p){float s=0.,a=.5;for(int i=0;i<5;i++){s+=a*n(p);p=p*2.02+7.3;a*=.5;}return s;}
vec2 cover(vec2 q,float zoom,vec2 pan){float ra=res.x/res.y,ia=isz.x/isz.y;vec2 s=ra>ia?vec2(1.,ia/ra):vec2(ra/ia,1.);q=(q-.5)*s/zoom+.5+pan;return vec2(q.x,1.-q.y);}`;

export const FRAGMENT: Record<LivePicture, string> = {
	// Curtains of light drift over the sky of the picture and mirror in the lake.
	nordlicht: `${COMMON}
float curtain(vec2 p){float x=p.x*2.2+fbm(vec2(p.x*1.5,t*.05))*1.8+t*.03;float band=fbm(vec2(x*3.,t*.08));float rays=pow(n(vec2(x*40.,t*.4)),3.);
float shape=smoothstep(.15,0.,abs(p.y-.28-.12*sin(x*1.3+t*.1)-.08*band));return shape*(.55+.9*rays)*band;}
void main(){float z=1.04+.02*sin(t*.04);vec2 uv=cover(v,z,-m*.01);vec3 col=texture2D(img,uv).rgb;float horizon=.64;float a=0.;
if(uv.y<horizon)a=curtain(uv);else{vec2 r=vec2(uv.x+.01*sin(uv.y*120.+t),2.*horizon-uv.y-.06);a=curtain(r)*.45*(1.-smoothstep(horizon,1.,uv.y));}
vec3 g=mix(vec3(.15,1.,.55),vec3(.45,.35,1.),smoothstep(.15,.45,uv.y));col+=g*a*.55;gl_FragColor=vec4(col,1.);}`,
	// The water ripples and glints a little, the clouds move, the view drifts.
	bergsee: `${COMMON}
void main(){float z=1.06+.03*sin(t*.05);vec2 pan=vec2(.012*sin(t*.03),.006*cos(t*.04))-m*.012;vec2 uv=cover(v,z,pan);float horizon=.505;float y=uv.y;vec2 q=uv;
if(y>horizon){float d=y-horizon;float w=fbm(vec2(uv.x*22.,y*90.-t*.6))-.5;float w2=n(vec2(uv.x*60.,y*240.+t*1.1))-.5;q.x+=w*.010*(.3+d*3.)+w2*.003;q.y+=w*.004*d*4.;}
else{float c=fbm(vec2(uv.x*3.+t*.012,uv.y*6.));q.x+=(c-.5)*.004-t*.0008*(1.-y/horizon);}
vec3 col=texture2D(img,fract(q)).rgb;
if(y>horizon){float s=pow(max(0.,n(vec2(uv.x*140.,y*500.+t*2.))-.72)*3.6,2.);col+=s*vec3(1.,.85,.7)*.12*smoothstep(horizon,horizon+.25,y)*smoothstep(1.,.6,y);}
gl_FragColor=vec4(col,1.);}`,
};
