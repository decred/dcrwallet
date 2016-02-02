& .\packages\OpenCover.4.6.166\tools\OpenCover.Console.exe -register:user "-filter:+[Paymetheus.Bitcoin]*" "-target:.\packages\xunit.runner.console.2.1.0\tools\xunit.console.exe" "-targetargs:Paymetheus.Tests.Bitcoin.dll" "-targetdir:Paymetheus.Tests.Bitcoin\bin\Debug"
& .\packages\ReportGenerator.2.4..0\tools\ReportGenerator.exe "-reports:results.xml" "-targetdir:.\coverage"
