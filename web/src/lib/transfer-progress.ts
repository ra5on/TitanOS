export type TransferSample={copied:number;time:number;active:boolean;speed:number};
export function transferSample(previous:TransferSample|undefined,copied:number,time:number,active:boolean):TransferSample{
 const seconds=previous?(time-previous.time)/1000:0;
 const delta=previous?copied-previous.copied:0;
 const speed=active&&previous?.active&&seconds>0&&seconds<=10&&delta>0?delta/seconds:0;
 return {copied,time,active,speed};
}
export function transferProblem(error:string){
 if(/not enough free space|no space left/i.test(error))return 'space';
 if(/may not be reached|address not allowed|redirect not allowed/i.test(error))return 'blocked';
 if(/SHA-256 does not match/i.test(error))return 'hash';
 if(/download returned HTTP/i.test(error))return 'http';
 if(/no write access|permission denied|read.only/i.test(error))return 'permission';
 if(/no such file|not found/i.test(error))return 'missing';
 if(/changed|conflict|already exists|name exists/i.test(error))return 'changed';
 if(/connection|network|timeout|timed out|stalled|unexpected EOF|download failed/i.test(error))return 'connection';
 return 'other';
}
