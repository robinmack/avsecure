#!/bin/bash
npm run build
sudo rm -rf /var/www/html/*


echo "Removed previous contents of /var/www/html"
sudo cp -r build/* /var/www/html/
echo "Copied new build"
sudo chmod -R 755 /var/www/html/*
echo "chmoded"
sudo chown -R nginx /var/www/html/*
echo "chowned to nginx. Deployed"
builddate=`date`
echo "Deployed at ${builddate}"
