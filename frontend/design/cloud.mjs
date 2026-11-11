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
    demo.signedIn = true;
    demo.account = {id: `preview:${name || email}`, email, name, githubLinked, balance:demo.claimed ? 600000 : 500000};
    return json(demo.account);
  };
  switch (path.slice('/api/cloud/'.length)) {
    case 'send-code':
      if (input.purpose === 'reset') demo.resetEmail = input.email;
      return json({exampleCode:'123456'});
    case 'verify-email':
      if (input.code !== '123456') return json({error:'invalid_credentials'}, 401);
      if (input.purpose === 'reset') {
        const account = demo.account ?? {email:'learner@example.com', name:'learner'};
        if (!account.name || input.email !== demo.resetEmail || account.email !== input.email || input.password?.length < 6 || !input.password) {
          return json({error:'invalid_credentials'}, 401);
        }
        delete demo.resetEmail;
        return signIn(account.email, account.name, account.githubLinked);
      }
      return demo.account?.email === input.email
        ? signIn(input.email, demo.account.name, demo.account.githubLinked)
        : signIn(input.email, input.username);
    case 'password': return demo.account?.name === input.username
      ? signIn(demo.account.email, input.username, demo.account.githubLinked)
      : signIn('learner@example.com', input.username);
    case 'register':
      if (!input.username || input.password !== input.confirmPassword || input.password.length < 6) return json({error:'invalid_account_input'}, 400);
      return signIn('', input.username);
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
        ? 'preview-email' : 'preview', authorizationUrl:'https://github.com/login/oauth/authorize?client_id=lll-design-preview'});
    case 'google-start':
      if (new URL(request.referrer || 'http://preview.invalid').searchParams.has('google-unavailable')) {
        return json({error:'google_unavailable'}, 503);
      }
      return json({flowId:'preview-google', authorizationUrl:'https://accounts.google.com/o/oauth2/v2/auth?client_id=preview.apps.googleusercontent.com'});
    case 'google-finish':
      return signIn('google-learner@example.com');
    case 'github-send-code':
      if (input.flowId !== 'preview-email') return json({error:'github_flow_expired'}, 400);
      demo.githubCodeSent = true;
      return json({exampleCode:'123456'});
    case 'github-finish':
      if (input.flowId === 'preview-email') {
        if (!input.code) return json({error:'github_email_confirmation_required'}, 409);
        if (!demo.githubCodeSent || input.code !== '123456') return json({error:'invalid_credentials'}, 401);
        delete demo.githubCodeSent;
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
