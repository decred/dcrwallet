// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

namespace Paymetheus.Bitcoin.Wallet
{
    public static class Accounting
    {
        public static bool RelevantTransaction(WalletTransaction tx, Account account, out Amount debit, out Amount credit)
        {
            debit = 0;
            credit = 0;
            var relevant = false;

            foreach (var input in tx.Inputs)
            {
                if (input.PreviousAccount == account)
                {
                    relevant = true;
                    debit -= input.Amount;
                }
            }
            foreach (var output in tx.Outputs)
            {
                var controlledOutput = output as WalletTransaction.Output.ControlledOutput;
                if (controlledOutput?.Account == account)
                {
                    relevant = true;
                    credit += output.Amount;
                }
            }

            return relevant;
        }
    }
}
