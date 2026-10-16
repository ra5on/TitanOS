// Minimal disposable guest used only by smoke-vm-snapshots.py. The state in
// RAM and on the virtual disk deliberately differ; both must survive restore.
#define _GNU_SOURCE
#include <dirent.h>
#include <fcntl.h>
#include <poll.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mount.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <sys/un.h>
#include <termios.h>
#include <unistd.h>
int main(int argc,char **argv){
 if(argc==3){
  int fd=socket(AF_UNIX,SOCK_STREAM,0);struct sockaddr_un address={.sun_family=AF_UNIX};snprintf(address.sun_path,sizeof address.sun_path,"%s",argv[1]);
  if(connect(fd,(void*)&address,sizeof address)){perror("connect");return 1;}
  dprintf(fd,"%s\n",argv[2]);struct pollfd p={fd,POLLIN,0};char buf[4096]={0};
  size_t used=0;
  while(used<sizeof(buf)-1 && poll(&p,1,10000)>0){int n=read(fd,buf+used,sizeof(buf)-1-used);if(n<=0)break;used+=n;buf[used]=0;if(strstr(buf,"SNAPSHOT-OK")){fputs(buf,stdout);return 0;}}
  fputs(buf,stdout);
  return 1;
 }
 mkdir("/dev",0755);mkdir("/proc",0755);mkdir("/sys",0755);mount("devtmpfs","/dev","devtmpfs",0,"");mount("proc","/proc","proc",0,"");mount("sysfs","/sys","sysfs",0,"");
 int fd=open("/dev/console",O_RDWR);dup2(fd,0);dup2(fd,1);dup2(fd,2);setvbuf(stdout,NULL,_IONBF,0);
 struct termios tty;if(!tcgetattr(0,&tty)){cfmakeraw(&tty);tcsetattr(0,TCSANOW,&tty);}
 for(int attempt=0;attempt<8;attempt++){
  DIR *dir=opendir("/modules");struct dirent *e;
  if(dir){while((e=readdir(dir))){if(e->d_name[0]=='.')continue;char path[512];snprintf(path,sizeof path,"/modules/%s",e->d_name);int f=open(path,O_RDONLY);if(f>=0){syscall(SYS_finit_module,f,"",0);close(f);}}closedir(dir);}
 }
 char ram[64]="boot",line[128],disk[64]={0};
 puts("SNAPSHOT-READY");
 for(;;){
  if(!fgets(line,sizeof line,stdin)){clearerr(stdin);sleep(1);continue;}
  char *newline=strchr(line,'\n');if(newline)*newline=0;
  if(!strncmp(line,"ram ",4))snprintf(ram,sizeof ram,"%.63s",line+4);
  fd=open("/dev/vda",O_RDWR|O_SYNC);
  if(fd<0){perror("disk");continue;}
  if(!strncmp(line,"disk ",5)){memset(disk,0,sizeof disk);snprintf(disk,sizeof disk,"%.63s",line+5);if(pwrite(fd,disk,sizeof disk,0)!=sizeof disk){perror("pwrite");close(fd);continue;}fsync(fd);}
  memset(disk,0,sizeof disk);pread(fd,disk,sizeof(disk)-1,0);close(fd);
  printf("RAM=%s DISK=%s SNAPSHOT-OK\n",ram,disk);
 }
 for(;;)pause();
}
