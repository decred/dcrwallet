& .\packages\OpenCover.4.6.519\tools\OpenCover.Console.exe -register:user `
	"-filter:+[Paymetheus.Decred]*" `
	"-target:.\packages\xunit.runner.console.2.1.0\tools\xunit.console.exe" `
	"-targetargs:Paymetheus.Tests.Decred.dll" `
	"-targetdir:Paymetheus.Tests.Decred\bin\Debug"

& .\packages\ReportGenerator.2.4.3.0\tools\ReportGenerator.exe `
	"-reports:results.xml" "-targetdir:.\coverage"
