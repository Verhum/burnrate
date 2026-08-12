module.exports = {
  apps: [
    {
      name: 'burnrate-daemon',
      script: './burnrate',
      args: 'serve',
      cwd: __dirname,
      env: {
        BURNRATE_PORT: 9113,
        BURNRATE_DATA_DIR:
          process.env.BURNRATE_DATA_DIR ||
          `${process.env.HOME}/.burnrate-dev`,
        BURNRATE_DEV_ORIGIN: 'http://localhost:3113',
        ...(process.env.DRY === '1' || process.env.BURNRATE_DRYRUN === '1'
          ? { BURNRATE_DRYRUN: '1' }
          : {}),
      },
      watch: ['burnrate'], // restart when binary changes
      watch_delay: 1000,
      autorestart: true,
      max_restarts: 10,
      restart_delay: 1000,
    },
    {
      name: 'burnrate-web',
      script: 'npm',
      args: 'run dev',
      cwd: __dirname + '/web',
      env: {
        BURNRATE_API_ORIGIN: 'http://127.0.0.1:9113',
      },
      autorestart: true,
    },
  ],
};
