import {fileURLToPath} from 'node:url';
import { defineConfig } from '@playwright/test';
export default defineConfig({
  testDir:'./tests',
  outputDir:'../../trash/verification/design-preview',
  timeout:90000,
  use:{baseURL:'http://127.0.0.1:5188'},
  webServer:{
    command:'npm run design', cwd:fileURLToPath(new URL('../',import.meta.url)),
    url:'http://127.0.0.1:5188',reuseExistingServer:true,
  },
});
