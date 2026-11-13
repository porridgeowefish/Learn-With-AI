export async function cloud({path, request, method, state, json}) {
  if (!path.startsWith('/api/cloud/')) return;
  const demo = state.cloud ??= { account: null, signedIn: false, claimed: false, selectedModel: '' };
  if (path === '/api/cloud/account') return json({
    signedIn: demo.signedIn, account: demo.signedIn ? demo.account : undefined, selectedModel: demo.selectedModel, dailyClaimed: demo.claimed,
    wallet: demo.signedIn ? {balance: (demo.claimed ? 600000 : 500000) - (demo.spent ?? 0), held: 0} : undefined,
  });
  if (path === '/api/cloud/google-config') return json({available:true});
  if (path === '/api/cloud/models') return json({models:[
    {id:'glm-5.3', name:'GLM-5.3'}, {id:'deepseek-flash', name:'DeepSeek V4.1 Flash'},
  ]});
  if (path === '/api/cloud/models/guest') return json({models:[
    {id:'glm-5.3', name:'GLM-5.3'}, {id:'deepseek-flash', name:'DeepSeek V4.1 Flash'},
  ]});
  if (path === '/api/cloud/daily-history') {
    if (!demo.signedIn) return json({error:'login_required'}, 401);
    const today = new Date(Date.now() + 8 * 3600000).toISOString().slice(0, 10);
    return json({today, entries: demo.claimed ? [{day: today, units: 100000}] : []});
  }
  if (method !== 'POST') return json({error:'unknown_cloud_action'}, 404);
  const input = await request.json();
  const signIn = (email, name, githubLinked = false) => {
    const existing = demo.account?.email === email ? demo.account : undefined;
    if (!existing) delete demo.password;
    demo.signedIn = true;
    demo.account = {id: existing?.id ?? `preview:${email}`, email, name: name ?? existing?.name,
      githubLinked: githubLinked || !!existing?.githubLinked, balance:demo.claimed ? 600000 : 500000};
    return json(demo.account);
  };
  switch (path.slice('/api/cloud/'.length)) {
    case 'send-code':
      if (!['register', 'reset'].includes(input.purpose)) return json({error:'invalid_account_input'}, 400);
      demo.emailChallenge = { email: input.email, purpose: input.purpose };
      return json({exampleCode:'123456'});
    case 'verify-email':
      if (!['register', 'reset'].includes(input.purpose)) return json({error:'invalid_account_input'}, 400);
      if (input.code !== '123456' || demo.emailChallenge?.email !== input.email || demo.emailChallenge.purpose !== input.purpose) return json({error:'invalid_credentials'}, 401);
      if (input.purpose === 'reset') {
        const account = demo.account ?? {email:'learner@example.com', name:'learner'};
        if (!account.name || account.email !== input.email || input.password?.length < 6 || !input.password) {
          return json({error:'invalid_credentials'}, 401);
        }
        delete demo.emailChallenge;
        const result = signIn(account.email, account.name, account.githubLinked);
        demo.password = input.password;
        return result;
      }
      {
        const username = input.username?.trim().toLowerCase();
        if (!/^[a-z0-9][a-z0-9_.-]{2,31}$/.test(username ?? '') || (input.password?.length ?? 0) < 6) return json({error:'invalid_account_input'}, 400);
        if (username === 'learner' || input.email === 'learner@example.com' || demo.account?.name === username ||
            (demo.account?.email === input.email && demo.account?.name)) return json({error:'account_conflict'}, 409);
        delete demo.emailChallenge;
        const result = signIn(input.email, username);
        demo.password = input.password;
        return result;
      }
    case 'password': {
      const username = input.username?.trim().toLowerCase();
      if (demo.account?.name && demo.account.name === username && input.password === (demo.password ?? 'demo-pass')) {
        return signIn(demo.account.email, username, demo.account.githubLinked);
      }
      if (!demo.account && username === 'learner' && input.password === 'demo-pass') {
        return signIn('learner@example.com', username);
      }
      return json({error:'invalid_credentials'}, 401);
    }
    case 'set-credentials': {
      if (!demo.signedIn || input.accountId !== demo.account.id) return json({error:'login_required'}, 401);
      if (demo.account.name) return json({error:'account_conflict'}, 409);
      const username = input.username?.trim().toLowerCase();
      if (!/^[a-z0-9][a-z0-9_.-]{2,31}$/.test(username ?? '') ||
          !input.password || input.password.length < 6 || input.password !== input.confirmPassword) {
        return json({error:'invalid_account_input'}, 400);
      }
      demo.account.name = username;
      demo.password = input.password;
      return json(demo.account);
    }
    case 'send-binding-email':
      if (!demo.signedIn) return json({error:'login_required'}, 401);
      if (demo.account.email) return json({error:'account_conflict'}, 409);
      demo.bindingEmail = input.email;
      return json({ok:true});
    case 'bind-email':
      if (!demo.signedIn) return json({error:'login_required'}, 401);
      if (demo.account.email) return json({error:'account_conflict'}, 409);
      if (input.email !== demo.bindingEmail || input.code !== '123456' || !input.password) return json({error:'invalid_credentials'}, 401);
      demo.account.email = input.email;
      demo.bindingEmail = undefined;
      return json(demo.account);
    case 'unbind-email':
    case 'unbind-github': {
      if (!demo.signedIn) return json({error:'login_required'}, 401);
      const email = path.endsWith('unbind-email');
      if (!demo.account.name || !(email ? demo.account.email : demo.account.githubLinked)) return json({error:'account_conflict'}, 409);
      if (!email && input.password !== 'demo-pass') return json({error:'invalid_credentials'}, 401);
      if (email) demo.account.email = '';
      else demo.account.githubLinked = false;
      return json(demo.account);
    }
    case 'github-start':
      if (input.link && !demo.signedIn) return json({error:'login_required'}, 401);
      return json({flowId: input.link ? 'preview-link' : new URL(request.referrer || 'http://preview.invalid').searchParams.has('github-email')
        ? 'preview-matched-email' : 'preview', authorizationUrl:'https://github.com/login/oauth/authorize?client_id=lll-design-preview'});
    case 'google-start':
      if (new URL(request.referrer || 'http://preview.invalid').searchParams.has('google-unavailable')) {
        return json({error:'google_unavailable'}, 503);
      }
      return json({flowId:'preview-google', authorizationUrl:'https://accounts.google.com/o/oauth2/v2/auth?client_id=preview.apps.googleusercontent.com'});
    case 'google-finish':
      return signIn('google-learner@example.com');
    case 'github-finish':
      if (input.flowId === 'preview-matched-email') {
        return signIn('learner@example.com', 'learner', true);
      }
      if (input.flowId === 'preview-link') {
        if (!demo.signedIn) return json({error:'login_required'}, 401);
        demo.account.githubLinked = true;
        return json(demo.account);
      }
      return signIn('github-learner@example.com', undefined, true);
    case 'logout':
      demo.signedIn = false;
      demo.bindingEmail = undefined;
      demo.selectedModel = '';
      return json({ok:true});
    case 'select-model':
      if (!demo.signedIn) return json({error:'login_required'}, 401);
      demo.selectedModel = input.model;
      return json({ok:true});
    case 'claim': {
      if (!demo.signedIn) return json({error:'login_required'}, 401);
      const granted = !demo.claimed;
      demo.claimed = true;
      return json({granted, balance:600000 - (demo.spent ?? 0), held:0});
    }
    default: return json({error:'unknown_cloud_action'}, 404);
  }
}
