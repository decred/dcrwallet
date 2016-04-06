// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Bitcoin;
using System;
using System.Diagnostics;

namespace Paymetheus.Rpc
{
    public static class WalletProcess
    {
        public static Process Start(BlockChainIdentity intendedNetwork, string appDataDirectory)
        {
            if (intendedNetwork == null)
                throw new ArgumentNullException(nameof(intendedNetwork));
            if (appDataDirectory == null)
                throw new ArgumentNullException(nameof(appDataDirectory));

            var networkFlag = "";
            if (intendedNetwork == BlockChainIdentity.TestNet3)
            {
                // While the actual name of the network is "testnet3", the wallet uses
                // the --testnet flag for this network.
                networkFlag = "--testnet";
            }
            else if (intendedNetwork != BlockChainIdentity.MainNet)
            {
                networkFlag = $"--{intendedNetwork.Name}";
            }

            var v4ListenAddress = RpcListenAddress("127.0.0.1", intendedNetwork);

            var processInfo = new ProcessStartInfo();
            processInfo.FileName = "btcwallet";
            processInfo.Arguments = $"{networkFlag} --noinitialload --experimentalrpclisten={v4ListenAddress} --onetimetlskey --datadir=\"{appDataDirectory}\"";
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

        public static string RpcListenAddress(string hostnameOrIp, BlockChainIdentity intendedNetwork)
        {
            if (intendedNetwork == null)
                throw new ArgumentNullException(nameof(intendedNetwork));

            string port;
            if (intendedNetwork == BlockChainIdentity.MainNet)
                port = "8332";
            else if (intendedNetwork == BlockChainIdentity.TestNet3)
                port = "18332";
            else if (intendedNetwork == BlockChainIdentity.SimNet)
                port = "18554";
            else
                throw new UnknownBlockChainException(intendedNetwork);

            return $"{hostnameOrIp}:{port}";
        }
    }
}
