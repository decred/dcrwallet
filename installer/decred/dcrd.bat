@echo off
echo "Starting dcrd..."
tasklist /nh /fi "imagename eq dcrd.exe" | find /i "dcrd.exe" >nul || start "dcrd" /D "%PROGRAMFILES%\Decred\dcrd" dcrd.exe
