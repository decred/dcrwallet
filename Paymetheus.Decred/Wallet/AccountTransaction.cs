// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred.Util;
using System;
using System.Collections.Generic;
using System.Linq;
using System.Text;
using System.Threading.Tasks;

namespace Paymetheus.Decred.Wallet
{
    public struct AccountTransaction
    {
        private AccountTransaction(WalletTransaction tx, Amount debit, Amount credit)
        {
            Transaction = tx;
            Debit = debit;
            Credit = credit;
        }

        public WalletTransaction Transaction { get; }
        public Amount Debit { get; }
        public Amount Credit { get; }
        public Amount DebitCredit => Debit + Credit;

        /// <summary>
        /// Creates an AccountTransaction for the account and wallet transaction when the transaction is relevant to the account.
        /// </summary>
        /// <param name="account">Account to search for in a transaction's inputs and outputs</param>
        /// <param name="tx">Wallet transaction to consider</param>
        /// <returns>An AccountTransaction for relevant transactions, otherwise null.</returns>
        public static AccountTransaction? Create(Account account, WalletTransaction tx)
        {
            Amount debit = 0, credit = 0;
            bool isForAccount = false; // Not saved by the WalletTransaction so need a way to detect irrelevant transactions.

            foreach (var input in tx.Inputs.Where(i => i.PreviousAccount == account))
            {
                debit -= input.Amount;
                isForAccount = true;
            }
            foreach (var output in tx.Outputs.OfType<WalletTransaction.Output.ControlledOutput>().Where(o => o.Account == account))
            {
                if (output.Change)
                    debit += output.Amount;
                else
                    credit += output.Amount;

                isForAccount = true;
            }

            return isForAccount ? new AccountTransaction(tx, debit, credit) : (AccountTransaction?)null;
        }
    }
}
