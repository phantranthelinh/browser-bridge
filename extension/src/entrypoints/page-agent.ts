import { defineUnlistedScript } from 'wxt/utils/define-unlisted-script';
import { install } from '../page-agent';

// Built as a standalone script that the service worker reads and runs in each page's isolated
// world through CDP (background/page.ts). It never runs as an extension page.
export default defineUnlistedScript(() => install());
