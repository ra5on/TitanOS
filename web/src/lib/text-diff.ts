export type DiffLine={before?:string;after?:string;beforeNumber?:number;afterNumber?:number;kind:'same'|'added'|'removed'|'changed'};
/** Bounded LCS; large unmatched blocks use a truthful positional comparison.
 * Common prefix/suffix are retained, and no quadratic work is done for large files. */
export function textDiff(before:string,after:string):{rows:DiffLine[];approximate:boolean}{
 const a=before.split('\n'),b=after.split('\n');
 let prefix=0,suffix=0;
 while(prefix<a.length&&prefix<b.length&&a[prefix]===b[prefix])prefix++;
 while(suffix<a.length-prefix&&suffix<b.length-prefix&&a[a.length-1-suffix]===b[b.length-1-suffix])suffix++;
 const n=a.length-prefix-suffix,m=b.length-prefix-suffix;
 const approximate=n*m>1_000_000;
 const rows:DiffLine[]=[];
 const same=(i:number,j:number)=>rows.push({before:a[i],after:b[j],beforeNumber:i+1,afterNumber:j+1,kind:'same'});
 for(let i=0;i<prefix;i++)same(i,i);
 let removed:number[]=[],added:number[]=[];
 function flush(){for(let k=0;k<Math.max(removed.length,added.length);k++){const i=removed[k],j=added[k];rows.push({before:i===undefined?undefined:a[i],after:j===undefined?undefined:b[j],beforeNumber:i===undefined?undefined:i+1,afterNumber:j===undefined?undefined:j+1,kind:i===undefined?'added':j===undefined?'removed':'changed'});}removed=[];added=[];}
 if(approximate){for(let i=0;i<n;i++)removed.push(prefix+i);for(let j=0;j<m;j++)added.push(prefix+j);flush();}
 else {
  const stride=m+1,table=new Uint32Array((n+1)*stride);
  for(let i=n-1;i>=0;i--)for(let j=m-1;j>=0;j--)table[i*stride+j]=a[prefix+i]===b[prefix+j]?1+table[(i+1)*stride+j+1]:Math.max(table[(i+1)*stride+j],table[i*stride+j+1]);
  let i=0,j=0;
  while(i<n||j<m){if(i<n&&j<m&&a[prefix+i]===b[prefix+j]){flush();same(prefix+i++,prefix+j++);}else if(i<n&&(j===m||table[(i+1)*stride+j]>=table[i*stride+j+1]))removed.push(prefix+i++);else added.push(prefix+j++);}
  flush();
 }
 for(let k=0;k<suffix;k++)same(a.length-suffix+k,b.length-suffix+k);
 return {rows,approximate};
}
