// deterministic ASCII field (seeded) so the OG renders identically every time
(function(){let s=1337;const r=()=>(s=(s*16807)%2147483647)/2147483647;
const G='{}();=+<>01solgnorealm.:-';let o='';for(let i=0;i<86*46;i++)o+=r()<.55?' ':G[(r()*G.length)|0];
document.getElementById('a').textContent=o;})();
