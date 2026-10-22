export const qk = {
 health:()=>['health'] as const,
 settings:{appearance:()=>['settings','appearance'] as const},
 projects:{all:()=>['projects'] as const,detail:(slug:string)=>['projects',slug] as const},
 preferences:{file:()=>['preferences'] as const},
 folders:{all:()=>['folders'] as const},
} as const;
