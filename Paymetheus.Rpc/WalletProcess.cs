// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Bitcoin;
using System;
using System.Diagnostics;

namespace Paymetheus.Rpc
{
    public static class WalletProcess
    {
        public static readonly string ApplicationDataDirectory =
            Portability.LocalAppData(Environment.OSVersion.Platform, "Btcwallet");

        public static Process Start(BlockChainIdentity intendedNetwork)
        {
            if (intendedNetwork == null)
                throw new ArgumentNullException(nameof(intendedNetwork));

            // Note: when mainnet becomes the default this should use "testnet" and
            // not "testnet3" as the flag.
            string networkFlag = "";
            if (intendedNetwork != BlockChainIdentity.TestNet3)
                networkFlag = $"--{intendedNetwork.Name}";

            var processInfo = new ProcessStartInfo();
            processInfo.FileName = "btcwallet";
            processInfo.Arguments = networkFlag + " --noinitialload";
            processInfo.UseShellExecute = false;
            processInfo.RedirectStandardError = true;
            processInfo.RedirectStandardOutput = true;
            processInfo.CreateNoWindow = true;

            var process = Process.Start(processInfo);
            process.ErrorDataReceived += (sender, args) => Console.WriteLine("err> {0}", args.Data);
            process.OutputDataReceived += (sender, args) => Console.WriteLine("{0}", args.Data);
            process.BeginErrorReadLine();
            process.BeginOutputReadLine();
            return process;
        }
    }
}
