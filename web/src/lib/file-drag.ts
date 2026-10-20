// The payload stays in this page's memory: external text/URLs cannot authorize a transfer.
export const fileDragType='application/x-titanos-files';
export type FileDrag={root:string;paths:string[]};
let active:FileDrag|null=null;
export function beginFileDrag(event:DragEvent,value:FileDrag){if(!event.dataTransfer)return;active={root:value.root,paths:[...value.paths]};event.dataTransfer.setData(fileDragType,'1');event.dataTransfer.effectAllowed='copyMove';}
export function endFileDrag(){active=null;}
export function hasFileDrag(event:DragEvent){return !!active&&!!event.dataTransfer?.types.includes(fileDragType);}
export function takeFileDrag(event:DragEvent){if(!hasFileDrag(event))return null;const result=active;active=null;return result;}
